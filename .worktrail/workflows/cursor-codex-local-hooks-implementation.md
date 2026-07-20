---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "cursor-codex-local-hooks-implementation",
  "scope": "project",
  "type": "workflow",
  "title": "Cursor 与 Codex 本地 Hooks 集成实施计划",
  "status": "active",
  "lifecycle": "current",
  "topic": "agent-integrations"
}
---

# Cursor 与 Codex 本地 Hooks 集成实施计划

## 1. 目标、来源与执行边界

本计划实施已提升的 `architecture/cursor-codex-local-hooks.md`：以本地 Worktrail CLI、项目级 native hooks 和用户级 Rules/Skills 提供 Cursor/Codex 的上下文注入、正式知识路径 guard、checkpoint、运行时恢复材料与可审计幂等。

本计划不实现 Plugin、Cloud Agent、后台服务、MCP server 或自动 handoff/promotion。架构中 ADR-HK-001 至 ADR-HK-005 均为 Proposed，不作为已接受 ADR 约束；现有 KDD compatibility decision 仅约束知识根和 candidate 生命周期，不改变本地 hook 实现。

Hook 配置直接调用 `worktrail hook <host> <event>`，不生成 wrapper、runner 或 manifest，也不对宿主平台额外设限。Cloud Agent 和 Plugin 分发明确不在本次范围。执行必须从新的隔离 git worktree 开始；现有 active state 的 plugin 分支名称源自已放弃的插件分发方向，不得作为实现约束。

## 2. 验收映射

| ID | 架构验收目标 | 实施阶段 |
|---|---|---|
| AC1 | 保留用户 hooks；旧 Codex 非 Worktrail scalar 不改写 | 1、2、6 |
| AC2 | 重复安装不创建重复 handler | 1、2、6 |
| AC3 | Codex matcher-group schema 与显式 timeout | 1、2、6 |
| AC4 | `init` 不隐式安装 hooks | 2、6 |
| AC5-7 | 唯一显式 active task、binding 生命周期、6 KiB context 上限 | 4、6 |
| AC8-9 | Shell/MCP 事前正式路径 guard、Cursor 文件编辑事后审计；受控 CLI 不受阻 | 3、5、6 |
| AC10-13 | terminal/checkpoint 幂等；不创建 takeover/handoff/candidate | 3、5、6 |
| AC14 | 不持久化 secret、原始 prompt/tool output 或绝对 transcript 路径 | 3、5、6 |
| AC15 | Doctor 发现 schema、receipt、legacy scalar 和 trust 问题 | 1、2、6 |
| AC16 | round-trip、protocol golden、并发、fail-open、全量与 race 验证 | 1-6 |

[parallelism:
- independent lanes: 阶段 1 的纯配置 reconciler 测试可与阶段 3 的宿主 payload/response golden fixture 准备并行；其余阶段依赖共享 contracts。
- sequential blockers: 先固定 config 与宿主协议 contracts，再接入 install；receipt 与 task binding 先于终止/compact cutover。
- shared write surfaces: `internal/hooks/`、`internal/integrations/`、`templates/config/*hooks.json`、`internal/store/init.go`。
- delegation: 0；配置协议、hook 输出和持久化 effect 共用同一契约，单一所有者可避免互相冲突。
]

## 3. 实施序列

### 1. 建立配置协调与 legacy cutover

**Landing**

- 新增 `internal/hookconfig/spec.go`、`internal/hookconfig/reconcile.go`、`internal/hookconfig/reconcile_test.go`
- 修改 `templates/config/cursor-hooks.json`
- 修改 `templates/config/codex-hooks.json`
- 修改 `internal/integrations/integrations.go`、`internal/integrations/integrations_test.go`

**Action**

- 用 `DesiredSpec` 表达 Cursor event-array 与 Codex event → matcher-group → handler 配置；handler identity 固定为 `host + event + worktrail-hook-command + contract-version`。
- 以 host 专用 reconcile 替代 `mergeJSONValue` 对 hooks 的整值合并；保留未知根字段、matcher、handler 与用户顺序。
- Cursor 将 managed handler append/upsert；Codex 在 event/group/handler 层 upsert。
- 识别旧 Worktrail Codex scalar command 并转换为新 matcher group；发现任意非 Worktrail scalar 时返回 `legacy_codex_user_hook_requires_manual_migration`，不改写 hooks。
- 将 templates 更新为架构定义的事件、matcher、timeout、直接 `worktrail hook <host> <event>` command 和 contract version。
- Uninstall 只移除 Worktrail identity，清理空 group，不删除用户配置。

**Verify**

- Cursor/Codex 空配置、用户 handler、重复 install、legacy Worktrail scalar、legacy 用户 scalar、malformed JSON 与 uninstall round-trip 单测。
- 对生成配置 JSON 做 golden 比较，并验证所有 Codex handler 带 timeout。

**Covers**：AC1、AC2、AC3、AC15、AC16。

### 2. 简化 project install 与 Doctor

**Landing**

- 修改 `internal/integrations/integrations.go`、`internal/integrations/integrations_test.go`
- 修改 `internal/store/init.go`、`internal/store/init_test.go`
- 修改 `internal/app/integration.go`、对应 CLI 测试

**Action**

- 保留现有 Rules/Skills 的 install/uninstall 语义；Cursor/Codex project install 在写入任何 Rules、Skills 或 hooks 前，先只读解析、legacy preflight 并构建目标 hooks JSON。旧非 Worktrail scalar 冲突时整个 project install 零写入。
- 预检通过后按现有顺序安装 Rules/Skills；仅 hooks 配置改为对项目根 `.cursor/hooks.json` 或 `.codex/hooks.json` 做原子 replace。replace 失败时保留原 hooks 配置，并在 report 中区分“Rules/Skills 已完成、hooks 未安装”的可重试状态。
- Uninstall 保留现有 Rules/Skills 清理语义；对同一 hooks 配置执行原子 replace，仅移除 Worktrail handler 与空 matcher group。
- 从 `store.InitProject` 删除 `mergeProjectCodexHooks`；`init` 只建立 Worktrail 根与 `.gitignore`。
- 扩展 `worktrail doctor cursor|codex --project`：检查 managed schema、直接 hook command、legacy scalar、重复 identity 与 Codex trust 提示；添加只读 receipt/binding 健康检查和显式 prune CLI 路由。

**Verify**

- 临时项目安装 smoke：验证生成配置仅包含预期的直接 `worktrail hook` commands，且 fixture 可由该 command 调用。
- 重复安装、legacy scalar 冲突、malformed JSON 与 hooks replace 失败分别验证：不产生重复 handler；legacy 冲突时 Rules/Skills 与 hooks 均零写入；replace 失败时原 hooks 不变且 report 明确可重试状态。
- `init` 后不存在 `.codex/hooks.json`；project install 后才存在。

**Covers**：AC1、AC2、AC3、AC4、AC15、AC16。

### 3. 以宿主原生协议替换通用 Hook Result

**Landing**

- 拆分 `internal/hooks/hooks.go` 为保留公共入口和新增 `internal/hooks/types.go`、`internal/hooks/cursor.go`、`internal/hooks/codex.go`、`internal/hooks/response.go`
- 修改 `internal/app/integration.go`
- 修改 `internal/hooks/hooks_test.go`
- 新增 `testdata/fixtures/hooks/cursor-*.json`、`testdata/fixtures/hooks/codex-*.json`

**Action**

- 定义 `NormalizedHookEvent` 与 `HookDecision`，将 Cursor/Codex payload 映射到共享字段，禁止 payload `project_id` 成为权威值。
- 固定 Cursor 事件矩阵：`sessionStart` 注入 context；`beforeShellExecution` 与 `beforeMCPExecution` 执行事前 guard；`afterShellExecution`、`afterMCPExecution` 与 `afterFileEdit` 仅审计；`preCompact` 写 checkpoint；`stop` 写终止 effect；`sessionEnd` 清 binding。不得注册不存在的 `preToolUse`、`postToolUse` 或 `subagentStop`。
- 固定 Codex 选择的九个事件：`PreToolUse`、`PermissionRequest`、`PostToolUse`、`PreCompact`、`PostCompact`、`SessionStart`、`UserPromptSubmit`、`SubagentStop`、`Stop`；显式不注册 `SubagentStart`，因为本计划没有对应 effect。
- 使用每事件 encoder，禁止把内部 `HookDecision` 直接作为通用 stdout wire：Cursor `sessionStart` 输出 `additional_context`；Cursor guard 输出 `permission`、`user_message`、`agent_message`；Codex `SessionStart`/`UserPromptSubmit` 输出带相应 `hookEventName` 与 `additionalContext` 的 `hookSpecificOutput`；`PermissionRequest` 与 `PreToolUse` 按各自官方 schema 生成 deny wire。
- Cursor guard 与 Codex `PreToolUse` deny 使用 exit `2`；Codex `PermissionRequest` deny 以其 `decision.behavior:"deny"` wire 表示并保持 exit `0`；成功 allow/no-op 使用 `0`。已捕获的解析或策略内部错误必须由 adapter 编码为该 event 的有效 allow/no-op JSON 并以 `0` 返回；仅无法序列化或写 stdout 的进程级故障允许非 `0/2`，Cursor 的该路径由官方 fail-open 语义覆盖，Codex 则须由 pinned fixture smoke 证明。移除 transcript tail、Cursor observed transcript registry 和通用 `Result` 输出。
- 将 project ID 从 `.worktrail/config.json` 读取，并对 payload 值仅做不一致诊断。

**Verify**

- 每个已注册宿主 event 的输入 fixture → stdout golden；Cursor `beforeShellExecution`/`beforeMCPExecution` 与 Codex `PreToolUse` deny 必须断言 JSON wire 和 exit `2`；Codex `PermissionRequest` deny 必须断言 decision wire 和 exit `0`；未知 event、malformed JSON 与可捕获内部错误必须断言有效 no-op JSON 和 exit `0`。进程级写出失败仅在 Cursor 断言非 `0/2` fail-open；Codex 必须以目标版本的 fixture smoke 验证其失败语义。
- 回归验证 hook 不写 primary state、candidate、handoff、takeover，且 runtime/log 不包含 prompt、tool output、transcript path 或 raw IDs。

**Covers**：AC10、AC13、AC14、AC16。

### 4. 实现唯一 active-task resolver、binding 与有界 context

**Landing**

- 修改 `internal/state/state.go`、`internal/state/state_test.go`
- 新增 `internal/hooks/binding.go`、`internal/hooks/context.go`、对应测试

**Action**

- 在 state 包新增只读查询：列出且仅返回带非空 task ID 的显式 active states；多于一个即为 ambiguous，不能使用 `LatestExplicit` 代替唯一性检查。
- 实现私有 binding 文件：host/session hash、state ID、task ID、state revision、last injected revision、last seen time；所有路径经 root-safe、无 symlink 校验。
- 仅在唯一 state 存在时创建/刷新 binding；state 关闭、变歧义、task ID 为空或同 task 不能唯一解析时删除。
- Codex `UserPromptSubmit` 在首次、revision 变化和 `PostCompact` 后注入；Cursor 只在 `sessionStart` 注入。使用 hooks 专用最小 renderer，不改变 `internal/contextpack` 的现有公开输出；固定字段、仓库相对引用和 6144 字节硬上限。
- 实现 Cursor session end 清理、Codex clear 重建与 24 小时闲置 binding 的显式 prune。

**Verify**

- 0/1/多 explicit active states、空 task ID、状态关闭、revision 变化、compact、session end、Codex clear 的表驱动测试。
- context 截断、无绝对路径/secret、只含允许字段以及 Cursor/Codex adapter 注入 JSON 测试。

**Covers**：AC5、AC6、AC7、AC14、AC16。

### 5. 实现 receipt 幂等、runtime effect 与路径 guard

**Landing**

- 新增 `internal/hooks/receipt.go`、`internal/hooks/receipt_test.go`
- 新增 `internal/hooks/guard.go`、`internal/hooks/guard_test.go`
- 修改 `internal/hooks/hooks.go`、`internal/runtime/runtime.go`、`internal/runtime/runtime_test.go`
- 必要时在 `internal/ops/` 增加仅 hooks 所需的受限 helper 与测试
- 新增 `internal/hooks/guard_test.go` 覆盖 `.worktrail` 根到知识相对路径的归一化；不修改 `internal/knowledge/formal.go`

**Action**

- 用 `.worktrail/ops/hook-receipts/sha256(effect-key).json` 实现 claimed/completed receipt；receipt claim、runtime/checkpoint 写入、事件日志与 complete 状态进入同一个 ops intent。
- effect key 按架构定义覆盖 terminal、checkpoint、validation 与 context；`sessionEnd` 只清 binding 与审计，移除 takeover 自动创建；`PreCompact` 写 checkpoint，`PostCompact` 只做 binding refresh/receipt 收尾。
- 复用 `knowledge.IsFormalKnowledgePath`，先把 API 文件路径归一化为 Worktrail 知识相对路径。Cursor 仅在 `beforeShellExecution` 与 `beforeMCPExecution` 事前阻断；`afterFileEdit` 只能产生最小 reason-code 审计，不宣称能撤销或阻止已完成的文件写入。
- 仅实现确定性 Shell grammar：单一文字目标的 `>`、`>>`、`tee`、`cp`、`mv`、`rm`，以及带 `path/file_path` 的写型 MCP；变量、glob、管道、多命令和未知 shape 只审计。
- 精确识别受控 `worktrail draft|adr|review` 允许路径；不以模糊字符串包含判断。

**Verify**

- 并发相同 effect 只产生一个 runtime/checkpoint；崩溃 failpoint 产生 pending intent，并仅能通过 `doctor ops repair --confirm` 恢复。
- receipt 的 30 天 retention/显式 prune、permissions 和 symlink-escape 测试。
- Cursor `beforeShellExecution`/`beforeMCPExecution`、Codex `PreToolUse`/`PermissionRequest` 的 supported Shell/MCP deny 与复杂 Shell audit-only 测试；Cursor `afterFileEdit` 仅写审计且不返回 deny；合法 Worktrail CLI 与 read 操作允许。
- `go test -race` 覆盖 hook/ops/runtime。

**Covers**：AC8、AC9、AC10、AC11、AC12、AC13、AC14、AC16。

### 6. Cutover 文档、迁移指导与端到端验证

**Landing**

- 修改 `docs/manual/AUTOMATION.md`、`docs/manual/INSTALLATION.md`、`docs/manual/TROUBLESHOOTING.md`
- 修改 `templates/config/*hooks.json` 的说明性测试和 `templates/skill_triggers_test.go`（若模板资产列表变化）
- 修改 `internal/integrations/integrations_test.go`、`internal/hooks/hooks_test.go`
- 新增或更新 `testdata/trigger-eval/cases.json` 与对应评测测试，仅在现有触发条件受 Hooks 语义影响时修改

**Action**

- `AUTOMATION.md` 说明 Hooks/Skills 边界、直接 CLI 调用、Cursor Shell/MCP 事前 guard 与文件编辑事后审计的能力边界；`INSTALLATION.md` 说明 project install、Codex `/hooks` trust 与配置 schema 更新后的重新 install；`TROUBLESHOOTING.md` 说明 legacy scalar、配置协调错误、Doctor/repair/prune 及不支持的 Cloud/Plugin 场景。
- 增加端到端测试：初始化 → project install → host fixture 直接调用 CLI → Doctor → uninstall；覆盖用户配置保留、迁移拒绝与原子 replace 失败。
- 对旧 Hooks 记录做迁移 smoke，确认不会自动创建 handoff、takeover 或 transcript metadata。

**Verify**

- `go test ./...`
- `go test -race ./...`
- `go build ./...`
- `git diff --check`
- 在隔离临时项目中，以生成的 Cursor/Codex 配置直接调用 `worktrail hook`，对 JSON fixture 运行协议 smoke。

**Covers**：AC1-AC16。

## 4. 风险、回退与增量

### 风险与缓解

| 风险 | 缓解 |
|---|---|
| 用户 Codex scalar 配置无法与新数组 schema 共存 | 安装前只读识别；非 Worktrail scalar 直接失败且零 hooks 写入；提供手动迁移提示。 |
| hooks 配置替换失败 | 仅在成功 preflight 后以原子 replace 写单个宿主配置；失败时原配置保持不变。 |
| hook 失败影响正常开发 | host adapter 输出有效 allow/no-op；宿主启动失败依赖其 fail-open 行为；Doctor 发现无效 managed handler 或配置 schema。 |
| receipt 与 runtime 部分写入 | 使用现有 ops intent；不自动 replay，显式 repair。 |
| guard 误报或绕过 | 只拦截可确定语法；复杂 Shell 审计；Cursor 文件编辑仅事后审计，CLI 与知识 review 继续是权威边界。 |
| 旧 runtime 隐私行为回归 | 删除 transcript-tail/observed-registry 路径并用负向 secret/path 测试覆盖。 |

### 回退

- host 或协议不兼容时卸载 Worktrail managed handler；不修改用户 handler。
- receipt、binding、runtime 是 ignored 本机材料；移除新 hook 后不影响正式知识和 Handoff V2 数据。
- 未决 ops intent 不自动处理；使用现有 `worktrail doctor ops repair --confirm`。

### 交付增量

1. 配置 reconciler + Doctor（可独立审核）。
2. 协议 adapter + task binding/context（仅在 fixture 下启用）。
3. receipt/guard/runtime cutover + 文档与全量验证。

## 5. 覆盖检查与后续

- 每项 AC 至少映射至一个实现阶段和验证；AC16 覆盖所有阶段。
- 新 package、CLI 子命令和 config schema 都必须在同一 PR 中包含 golden/round-trip 测试，避免仅靠手册约束。
- 完成后先运行 self-review、再执行 targeted validation；由于涉及宿主协议、文件权限和并发，实施后必须进行 code review，不应直接发布。

下一步：本计划通过审核后，在新的隔离 worktree 依序实施。
