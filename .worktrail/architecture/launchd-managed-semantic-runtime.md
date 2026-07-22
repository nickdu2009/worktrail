---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "launchd-managed-semantic-runtime",
  "scope": "project",
  "type": "architecture",
  "title": "Worktrail launchd 托管语义运行时架构",
  "status": "active",
  "lifecycle": "current",
  "topic": "semantic-runtime"
}
---

# Worktrail launchd 托管语义运行时架构

## 1. 背景与目标

当前 Darwin semantic worker 由短命 CLI 启动，仍属于调用方操作系统进程组；受管执行器在 CLI 退出时会回收该进程组，造成 worker 死亡和 stale descriptor。目标是以 macOS `launchd` 托管同一登录用户共享的 Semantic Host，使 CLI、Codex 和 Cursor 跨命令复用一个受信 worker，并在空闲后释放模型内存。

本架构只替换 runtime 生命周期与调用接缝。Markdown/frontmatter 真相、内容寻址 bundle、rebuild-only generation、chunking、ranking、hybrid recall 和显式 rebuild 合同保持不变。

## 2. 已确认边界

- 服务作用域：`gui/<uid>` 用户级单实例。
- 服务管理：LaunchAgent label `com.nickdu2009.worktrail.semantic`。
- 资源策略：按需 kickstart，默认空闲 10 分钟退出，可配置 1-60 分钟。
- worker：首期单 worker；切换前先 drain，不支持两个 bundle 同时 warm。
- 平台：协议与操作合同跨平台，首期只实现 Darwin；headless/Linux/Windows 返回现有 platform unsupported。
- 依赖：纯 Go，复用现有依赖；无 helper binary、CGo、XPC、插件或 provider registry。

## 3. 组件与依赖

```text
app/search/rebuild
        |
        v
composition -> service.Client
                    |
          HTTP/1.1 over private UDS
                    |
                    v
             service.Host
                    |
                    v
        daemon.Supervisor + HTTPClient
                    |
                    v
             llama serve worker
```

- `daemon`：继续拥有 worker supervisor、descriptor、进程身份、endpoint allocator、readiness 与 llama HTTP client；不依赖 service。
- `service`：拥有 Host、UDS protocol/client、launchd manager、idle lease 和服务配置；单向依赖 daemon。
- `composition`：分别组装客户端和 Host依赖。
- `app/search/rebuild`：继续消费现有 `daemon.Controller`、`contracts.TokenCounter`、`generation.Embedder` 和 `retrieve.QueryEmbedder`；不读取 worker endpoint或API key。

不新增 `SemanticRuntimeClient` Go interface。具体 `service.Client` 通过方法集满足现有接口。Darwin/unsupported manager使用同一具体类型和 build-tag文件。

## 4. 文件与配置

复用 `paths.SemanticRoots`：

```text
Cache/runtime/semantic-host.sock
Cache/runtime/activation.lock
Runtime/service.json
Runtime/service-metadata.json
Logs/
~/Library/LaunchAgents/com.nickdu2009.worktrail.semantic.plist
```

`service.json` schema为 `worktrail.semantic.service-config.v1`，仅配置 `idle_timeout`。缺失时默认 `10m`；合法范围 `1m` 至 `60m`；非法配置 fail closed，不自动覆盖。runtime目录为 `0700`，配置与metadata为 `0600`。

## 5. launchd 合同

plist固定字段：

```text
RunAtLoad = false
KeepAlive = false
ProcessType = Standard
ExitTimeOut = 10
AbandonProcessGroup = false
ProgramArguments = [<absolute-worktrail>, semantic, host, --launchd]
```

安装仅由显式 `worktrail init --semantic` 执行。manager原子写plist和metadata，验证owner、type和permission，使用 `bootstrap gui/<uid>` 注册。普通激活使用不带 `-k` 的 `kickstart`；显式 restart仅在Worktrail label和metadata验证成功后允许 `kickstart -k`。生产逻辑只使用launchctl退出状态，不解析输出文本。

同label但内容或owner不匹配时fail closed。`semantic service uninstall --confirm` 执行bootout并删除Worktrail自有plist、metadata和UDS，保留bundle、generation和用户`service.json`。

## 6. 私有 Host 协议

协议为 version 1 的 typed HTTP/1.1 over UDS：

```text
GET  /v1/status
POST /v1/runtime/start
POST /v1/runtime/stop
POST /v1/runtime/restart
POST /v1/tokenize
POST /v1/embeddings
```

请求包含 protocol version、request ID、bundle ID和endpoint专用payload；响应包含 protocol version、Host build ID、bundle/runtime identity、service/worker state及typed result/error。请求和响应各限制为1 MiB。协议不兼容时客户端不得发送query、chunk或其他知识正文。

UDS父目录 `0700`、socket `0600`，Host验证peer UID。客户端从不删除socket。只有被launchd启动的新Host在bind得到`EADDRINUSE`、连接明确失败、owner/type/parent permission验证通过时，才依赖launchd单实例保证删除stale socket并重试一次。

## 7. Host与worker生命周期

- status不激活Host、不启动worker、不持有idle lease。
- start、tokenize和embed可激活Host；generation/profile预检必须先完成。
- worker启动前及复用前完整校验trusted bundle。
- worker绑定随机authenticated loopback；API key由Host独占，不暴露给客户端、plist或日志。llama要求的临时key文件为`0600`并在worker停止时删除。
- worker留在Host的launchd进程组，不设置`Setsid`；request context取消不得直接杀worker。
- 一个Host只管理一个worker。不同bundle请求必须先drain并停止现有worker，否则返回capacity error。
- tokenize/embed持有idle lease；worker启动期间暂停idle判断。
- 最后一个lease释放后开始idle timeout。到期后停止接收新请求，等待已有请求结束，验证PID/start-time后SIGTERM，最多等待5秒，必要时只对同一身份SIGKILL，删除key/诊断状态/UDS并退出。
- worker crash对一个请求最多恢复一次；禁止重启循环。

## 8. 客户端激活与错误

```text
直接请求UDS
-> missing/refused：取得短期激活锁
-> 锁内再请求一次
-> service注册有效：launchctl kickstart
-> 等待最多5秒完成Host handshake
-> 原请求重试一次
```

HTTP 4xx、协议不兼容、bundle/profile mismatch、输入过大、generation错误和inference timeout不触发自动重启。

新增稳定reason：

```text
semantic_service_not_installed
semantic_service_unavailable
semantic_service_incompatible
```

保留现有bundle、runtime、generation、profile和capacity reasons。默认lexical不检查service；auto公开degraded reason并继续lexical；required稳定失败。缺少`gui/<uid>`复用`semantic_platform_unsupported`并说明headless当前不支持。

## 9. Status与诊断

继续使用 `worktrail.semantic.status.v1`，保留现有字段和含义，只追加可选字段：service registration/domain、Host protocol/build/state、worker state、active bundle、PID/start-time、active requests、last completion、idle timeout/deadline、cold-start latency和last failure。

`registered + host stopped + worker idle_stopped` 是正常冷状态。descriptor只保存诊断，不再作为liveness或信任权威。日志只记录request ID、bundle ID、状态、耗时和错误码，不记录正文、向量或凭证。

## 10. 安装、升级、迁移与回滚

- M1 verified bundle安装后修复LaunchAgent并只验证Host handshake，不加载模型。
- experimental bundle继续使用现有 `InstallAndCheck`，通过Host执行readiness/tokenize/embed。
- service注册失败保留bundle并恢复旧plist/metadata；experimental runtime自检失败恢复service后由现有installer拒绝新bundle；回滚失败时保留bundle现场。
- 升级先认证并drain兼容Host，再bootout、原子替换和bootstrap；失败恢复旧plist/metadata。
- legacy descriptor仅在UID、PID/start-time、API key、alias、runtime fingerprint和bundle全部匹配时受控停止。PID不存在时删除stale记录；身份不确定时隔离记录并返回identity mismatch，绝不发信号。
- 回滚新版先执行 `semantic service uninstall --confirm`；bundle、generation和知识保留。

## 11. 验收标准

- 20个并发客户端只有一个Host和一个worker。
- CLI退出后另一CLI能继续复用worker。
- idle timeout后Host和worker退出，下一请求能恢复。
- Host被SIGKILL后没有llama孤儿；worker crash只恢复一次。
- status、默认lexical和generation预检失败不激活Host。
- stale socket、wrong owner、协议不兼容和unknown PID不被误删或停止。
- 两个独立required查询均返回`semantic-retrieve-v2`与`vector_knn`。
- blocked outbound下tokenize/embed正常。
- install/update/uninstall幂等且更新失败可恢复。
- headless可见降级且不启动裸daemon。
- Darwin真实launchd gate、现有production E2E、`go test ./...`和`go vet ./...`通过。

## 12. ADR索引

| ID | 状态 |
| --- | --- |
| ADR-20260722-launchd-managed-semantic-host | Accepted |
| ADR-20260715-local-semantic-runtime-bundle | Accepted |
| ADR-20260715-rebuild-only-semantic-generation | Accepted |
| ADR-20260715-hybrid-recall-context-contract | Accepted |
