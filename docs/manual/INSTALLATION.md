# 安装说明
<div class="title-en">Installation</div>

## 选择安装层次
<div class="title-en">Choose an Install Layer</div>

这部分的重点不是把所有命令堆在一起，而是帮助你先选对层次。

Worktrail 有三件事需要区分：

- CLI：本机可执行的 `worktrail` 命令
- Worktrail scope：用户级 `~/.worktrail/` 和项目级 `.worktrail/`
- Agent 集成：给 Codex、Claude Code、Cursor、ZCode Agent 安装规则、skills、hooks 或工具配置

普通使用者可以按这个顺序理解：

1. 先让 `worktrail` 命令可用。
2. 在需要使用的仓库里运行 `worktrail init`。
3. 如果希望 Agent 自动遵循 Worktrail 工作流，再安装对应 Agent 集成。

## 安装 CLI
<div class="title-en">Install the CLI</div>

先安装 Worktrail CLI，使下面命令可以正常运行：

```bash
worktrail --help
```

如果你使用发布包，按发布包说明把二进制加入 `PATH`。如果你在源码仓库中开发，使用项目当前提供的 Go 入口或构建脚本安装 CLI；安装完成后再回到本手册继续。

Agent 集成依赖 `worktrail` 命令本身。请先确保 CLI 可用，再运行 `worktrail install ...`。

## 初始化 Worktrail
<div class="title-en">Initialize Worktrail</div>

### 初始化用户级和项目级
<div class="title-en">Initialize User and Project Scope</div>

在目标仓库根目录运行：

```bash
worktrail init
```

这会初始化用户级和项目级 Worktrail 数据。项目级 `.worktrail/` 存在时，安装到用户级的 Worktrail skills 才会在该项目中自动运行常规工作流。

### 可选安装本地语义召回
<div class="title-en">Optional Local Semantic Recall Installation</div>

核心初始化默认不访问网络，也不会自动下载语义模型或 runtime。只有显式请求
`worktrail init --semantic` 才会安装本地语义 bundle；`worktrail init
--no-semantic` 会明确禁用语义安装。安装成功后仍需显式执行
`worktrail semantic rebuild --scope all` 才会创建索引。

M1 是唯一的 `verified` runtime 变体。M2–M5 是 opt-in `experimental`
变体：仅使用当前芯片自己的 pinned official artifact，并在安装时通过本地
integrity、authenticated-loopback、alias、tokenization、embedding shape、
CLS pooling 和 L2-normalization self-check 后才能激活。自检失败会拒绝该
bundle；`auto` mode 显式降级到 lexical，不会回退到其他芯片 artifact。

`experimental` 不表示 `compatible` 或 `verified`，也不承诺性能、隐私、
最低 macOS 或运营支持。支持层级与发行门禁见
[`docs/worktrail-release-acceptance.md`](../worktrail-release-acceptance.md)。

### 只初始化用户级
<div class="title-en">User Scope Only</div>

如果你只想准备用户级知识、状态和候选目录：

```bash
worktrail init-user
```

### 只初始化项目级
<div class="title-en">Project Scope Only</div>

如果你只想让当前仓库具备项目级 Worktrail 根目录：

```bash
worktrail init-project
```

## 安装 Agent 集成
<div class="title-en">Install Agent Integrations</div>

Worktrail 当前支持 `codex`、`claude`、`cursor`、`zcode` 和 `all`。

### 用户级集成
<div class="title-en">User Scope Integration</div>

用户级安装适合个人跨项目复用：

```bash
worktrail install cursor --user
worktrail install codex --user
worktrail install claude --user
worktrail install zcode --user
```

默认未指定 scope 时，`worktrail install <tool>` 等价于用户级安装。

用户级安装会写入跟随个人环境的规则和 skills。Cursor 用户级安装会把 Worktrail rule 和 skills 写入 Cursor 自己的用户目录；如果已有兼容的 `$HOME/.agents/skills`、`$HOME/.codex/skills` 或 `$HOME/.claude/skills`，`doctor cursor --user` 会把这些重复可见副本提示为 warning，但不会阻止安装。ZCode Agent 的用户级安装会写入 `~/.zcode/AGENTS.md` 和 `~/.zcode/skills/`，并通过这些指令与技能把 Worktrail 自动化表达成 Agent 的语义路由，而不是项目级 hooks。

### 项目级集成
<div class="title-en">Project Scope Integration</div>

项目级安装适合让当前仓库带上运行时集成配置：

```bash
worktrail install cursor --project
worktrail install codex --project
worktrail install claude --project
```

项目级安装主要写入当前仓库需要的 hooks、settings 配置和 `.gitignore` 运行时条目。它不会从 `templates/root/**` 或 `templates/skills/**` 安装项目级规则或项目级 skills。ZCode Agent 当前没有 Worktrail-managed 的项目级 hooks 或 runtime settings，因此不需要 `worktrail install zcode --project`。

Cursor/Codex 项目 hooks 使用宿主原生 schema，并直接调用 `worktrail hook <host> <event>`：

- Cursor：event array；guard 事件 timeout 1 秒，其他 2 秒
- Codex：event → matcher group → command handler；同样要求显式 timeout
- 安装会保留用户已有 handler；重复安装不会复制 Worktrail managed handler
- 若 Codex hooks 仍是非 Worktrail 的旧 scalar 字符串，整个 project install 会零写入并要求手动迁移
- `worktrail init` 不会写入 `.codex/hooks.json` 或 `.cursor/hooks.json`

Codex 安装后需在 Codex 内用 `/hooks` 确认项目 hooks 已信任，否则 managed handler 不会生效（trust 状态不可机器检测；`doctor codex` 以 manual-only check 提示）。升级 hooks schema 后请重新执行 `worktrail install cursor|codex --project`。

### 一次安装用户级和项目级
<div class="title-en">Install Both Scopes</div>

如果你想完整接入某个工具：

```bash
worktrail install cursor --user --project
```

如果你想完整接入 ZCode Agent，当前只需要用户级安装：

```bash
worktrail install zcode --user
```

如果你要同时接入 Codex、Claude Code、Cursor 和 ZCode Agent：

```bash
worktrail install all --user --project
```

这条命令会同时安装 ZCode Agent 的用户级集成；ZCode Agent 当前不会额外生成项目级 hooks 或 settings。

## 检查安装
<div class="title-en">Check the Installation</div>

检查当前路径发现的用户级和项目级 Worktrail 根目录：

```bash
worktrail doctor
```

检查某个 Agent 集成：

```bash
worktrail doctor cursor --user
worktrail doctor cursor --project
worktrail doctor codex --user --project
worktrail doctor claude --user --project
worktrail doctor zcode --user
```

检查知识治理问题：

```bash
worktrail doctor knowledge
```

这条命令主要检查正式知识的治理漂移，例如分类混放、`source_of_truth` 冲突、starter 文档仍引用 superseded 文档、索引过期，以及绕过 review 流的直接 formal knowledge 编辑。Handoff V2 是独立的 runtime recovery surface，不属于 formal knowledge 或 candidate review；安装后可单独运行：

```bash
worktrail handoff doctor
```

Local handoff 默认保存在 `.worktrail/handoffs/local/`，仅供本机恢复。只有显式 `worktrail handoff publish <local-id>` 才写入可由 Git 分享的 `.worktrail/handoffs/team/`。Publish 本身不会 stage、commit 或 push，doctor 会报告尚未被 Git 跟踪的 team 文件。

## 卸载集成
<div class="title-en">Uninstall Integrations</div>

如果需要移除某个 Agent 集成：

```bash
worktrail uninstall cursor --user
worktrail uninstall cursor --project
worktrail uninstall zcode --user
```

卸载集成只处理对应集成写入的托管文件，不等同于删除 `.worktrail/` 中的知识、状态或候选记录。

## 安装后的第一轮验证
<div class="title-en">First Validation</div>

安装完成后建议做三件事：

- 运行 `worktrail context "test task"`，确认能生成上下文包
- 运行对应 `worktrail doctor <tool> --user|--project`，确认集成文件存在
- 在真实小任务里观察 Agent 是否会在开始、长任务中和结束前使用 Worktrail skills 或等价命令

如果第一步就失败，先看[故障排查](TROUBLESHOOTING.md)。
