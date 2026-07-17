# Worktrail 设计审核：移除类似 TUI 的额外复杂面

## 2026-07-15 语义召回范围修订

本文件保留原始复杂度审核依据，但“完全不做 daemon/vector”的结论已被
`worktrail-local-semantic-recall-architecture.md` 的受限例外取代：

- 不引入通用 Worktrail daemon、watcher、公共 HTTP API 或独立向量服务。
- 只有显式安装和显式调用的本地语义能力可以按需启动受认证、仅 loopback
  的 llama.app 进程。
- sqlite-vec generation 是可删除、可重建的派生索引，不是 source of truth。
- 该例外不得执行知识治理写操作，失败时现有 lexical 工作流仍然可用。

## 审核原则

本次审核按三条标准判断是否存在类似 TUI 的问题：

1. 是否引入额外界面：需要用户离开 Codex / Claude Code 的新 UI。
2. 是否引入额外运行面：常驻服务、端口监听、后台 watcher、HTTP server。
3. 是否引入额外权限面：AI 可以绕过用户确认直接修改正式知识库。

## 结论

原设计中除了 TUI，还有几类同类风险。已在 reviewed 版本文档中收敛。

| 风险点 | 问题 | 处理结果 |
|---|---|---|
| TUI | 额外终端界面，脱离 Codex / Claude Code | 移除；改为 chat-native review |
| HTTP MCP server | 引入端口、服务生命周期、安全边界 | 移除；MCP 只支持 stdio |
| 后台 daemon / watcher | 引入常驻进程和隐式行为 | 通用 daemon/watcher 仍不做；仅允许显式语义操作管理的受限 llama.app 进程 |
| MCP promote / merge / discard | AI 工具层可能绕过人工确认修改正式知识库 | 默认不暴露；写操作通过非交互 CLI，在用户确认后由 skill 调用 |
| 本地 embedding / vector index | 增加复杂依赖，偏离 Markdown 本地工具定位 | 作为 opt-in v1 能力重新引入；必须本地、可重建、可审计并保留 lexical 基线 |
| custom external command provider | 可能造成 arbitrary command execution | 移除；只支持 manual / codex / claude provider |
| hooks 自动 promote | 自动化越过人审 | 明确禁止；hooks 只能生成 candidates 或更新 state |
| 自动后台 sync | 隐式扫描用户文件和 transcript | 明确不做；sync 只能手动或由 hooks 触发 |

## 新的边界

```text
CLI = 核心执行层
Hooks = 自动采集 / checkpoint / candidate 生成
Skills = Codex / Claude Code 对话内流程入口
MCP stdio = Agent 读上下文和生成 draft 的工具层
Codex / Claude Code chat = 人工 review 界面
Markdown = source of truth
```

## 明确不做

```text
1. 不做 TUI
2. 不做 Web UI / dashboard
3. 不做 HTTP MCP server
4. 不做通用后台 daemon / watcher；语义 runtime 只能按显式语义操作启动
5. 不做云 embedding 或独立向量数据库；本地 sqlite-vec 仅作派生索引
6. 不做 custom external command provider
7. 不通过 MCP 默认暴露 promote / merge / discard
8. 不允许 hooks 自动 promote
9. 不做自动后台 watcher
```

## 保留项说明

以下能力保留，因为它们是 Codex / Claude Code 原生集成所需，不属于“额外界面”：

- `AGENTS.md` / `CLAUDE.md`：稳定入口。
- hooks：自动捕捉 session、tool use、compact、stop 等事件。
- skills：`/worktrail-context`、`/worktrail-review` 等对话内工作流。
- MCP stdio：由 Codex / Claude Code 启动的本地工具协议，不监听端口。
- local text index：为 `context` 和 `search` 提供本地检索能力，可重建，不是 source of truth。

## Review 新流程

```text
1. hooks / extract 生成 candidates
2. 用户在 Codex / Claude Code 中运行 /worktrail-review
3. Agent 通过 CLI/MCP 读取 candidate 列表和 diff
4. 用户在对话里明确确认 promote / merge / discard
5. Agent 调用非交互 CLI 执行写操作
6. worktrail 执行 redaction scan、path check、backup、event log、index rebuild
```

