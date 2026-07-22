---worktrail
{
  "created_at": "2026-07-22T02:54:02.999923Z",
  "id": "ADR-20260722-launchd-managed-semantic-host",
  "lifecycle": "current",
  "schema": "worktrail.knowledge.v1",
  "scope": "project",
  "stage": "decision",
  "status": "accepted",
  "title": "使用 launchd 托管用户级 Semantic Host",
  "type": "decision",
  "updated_at": "2026-07-22T02:54:02.999923Z"
}
---

# ADR-20260722-launchd-managed-semantic-host: 使用 launchd 托管用户级 Semantic Host

- Status: Accepted
- Date: 2026-07-22

## Context

Worktrail 本地语义检索依赖受信 bundle、llama runtime 和 rebuild-only generation。当前 Darwin 实现由短命 CLI 启动 worker；即使 Go context 已与调用方取消解耦，worker 仍处于调用方操作系统进程组。受管执行器在 CLI 退出时回收该进程组，造成 runtime 死亡并遗留 stale descriptor。

需要一个同登录用户共享的单实例服务，使 CLI、Codex 和 Cursor 能复用语义 runtime，并在空闲后释放模型内存。首期只实现 macOS GUI login domain，同时保持操作和协议合同可由未来平台映射。

## Decision Drivers

- runtime 必须由长生命周期用户服务管理器拥有。
- status 不得激活服务或加载模型。
- 首次语义请求只允许一次受控激活与一次原请求重试。
- 必须复用现有 bundle trust、rebuild-only generation 和 hybrid recall 合同。
- 未知进程、socket 或 descriptor 永不被终止或覆盖。
- 首期不增加 CGo、Swift/XPC、helper binary 或第三方 service-manager 依赖。

## Considered Alternatives

### CLI worker 加 setsid

不采用。它不能提供用户级单实例、可靠升级、卸载和空闲回收。

### 每个 bundle 一个 launchd worker

不采用。会把端口、凭证、升级和路由扩散为多实例管理问题。

### launchd socket activation 或 Swift/XPC

首期不采用。前者需要额外互操作，后者增加语言、签名和发布边界。

### launchd 托管单一 Go Host

采用。它使用原生用户服务所有权，同时可复用现有 Supervisor 和纯 Go UDS HTTP。

## Decision

1. 安装用户级 LaunchAgent：domain `gui/<uid>`，label `com.nickdu2009.worktrail.semantic`，`RunAtLoad=false`、`KeepAlive=false`、`ProcessType=Standard`、`ExitTimeOut=10`、`AbandonProcessGroup=false`。
2. 同一个 `worktrail` 可执行文件提供内部入口 `semantic host --launchd`；不增加 helper binary。
3. 客户端使用 versioned HTTP/1.1 over private UDS。UDS 为 `os.UserCacheDir()/worktrail/semantic/runtime/semantic-host.sock`，父目录 `0700`、socket `0600`，Host 校验 peer UID。
4. status 只检查 service metadata 和已有 UDS；start、tokenize、embed、search/context semantic lane 与 rebuild 才可执行一次 `launchctl kickstart` 激活。
5. Host 复用现有 bundle verifier、llama HTTP client、endpoint allocator、start lock 和 Supervisor。worker 留在 Host 的 launchd 进程组，不使用 `setsid`。
6. 首期一个 Host 只管理一个受信 worker。切换 bundle 或升级时先 drain 并停止旧 worker，再启动新 worker；双 worker 并存留待未来有两个并存受信 bundle 时另立 ADR。
7. worker API key 由 Host 独占且不暴露给客户端、plist 或日志；允许 llama `--api-key-file` 所需的生命周期内 `0600` 临时文件，worker 停止时删除。
8. 每个 tokenize/embed 请求持有 idle lease。最后一个 lease 释放后开始计时；用户级 `service.json` 默认 `10m`，允许 `1m` 至 `60m`。超时后 Host 停止 worker、删除 UDS 并退出。
9. worker 停止前验证 PID/start-time；SIGTERM 后最多等待 5 秒，必要时只对相同身份 SIGKILL。Host 异常退出或登出时由 launchd 进程组清理兜底。
10. legacy descriptor 不再是 liveness 权威。仅在 UID、PID/start-time、API key、alias、runtime fingerprint 与 bundle 全部匹配时受控停止；身份不确定时隔离记录、返回 identity mismatch、绝不发信号。
11. 缺少 `gui/<uid>` 时复用 `semantic_platform_unsupported` 并报告 headless session 当前不支持；不恢复裸 daemon。
12. 跨平台边界是固定的 service 操作与 Host 协议合同。首期使用具体 Go 类型与 build-tag 文件，不建设 provider registry、插件或多平台工厂。
13. 保持 Markdown 真相、内容寻址 bundle、rebuild-only generation、默认 lexical、`semantic=auto` 可见降级和 `semantic=required` fail-closed 不变。

## Consequences

### Positive

- CLI 退出不再决定 runtime 生命周期。
- 同用户客户端共享一个 Host 和一个 worker。
- launchd 提供异常退出、登出和进程组清理。
- 空闲计时提供可预测的内存释放。
- 不增加语言、二进制或第三方依赖。

### Negative

- Worktrail 需要维护 LaunchAgent、私有协议、升级和卸载。
- 首次请求包含 Host 激活和模型冷启动延迟。
- 首期不支持 headless macOS、Linux 或 Windows semantic runtime。
- 协议不兼容时必须受控修复，不能混用新旧客户端。

## Revisit Conditions

- 需要同时信任并服务两个 bundle；
- 正式支持 Linux、Windows 或 headless macOS；
- launchctl kickstart 的真实可靠性无法满足发布门槛；
- Go 工具链可低成本使用 launchd socket activation；
- Host 代理经测量成为显著瓶颈。

## Links

- Related: ADR-20260715-local-semantic-runtime-bundle
- Related: ADR-20260715-rebuild-only-semantic-generation
- Related: ADR-20260715-hybrid-recall-context-contract
