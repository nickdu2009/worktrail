# 安装说明
<div class="title-en">Installation</div>

## 选择安装层次
<div class="title-en">Choose an Install Layer</div>

这部分的重点不是把所有命令堆在一起，而是帮助你先选对层次。

Worktrail 有三件事需要区分：

- CLI：本机可执行的 `worktrail` 命令
- Worktrail scope：用户级 `~/.worktrail/` 和项目级 `.worktrail/`
- Agent 集成：给 Codex、Claude Code、Cursor 安装规则、skills、hooks 或工具配置

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

Worktrail 当前支持 `codex`、`claude`、`cursor` 和 `all`。

### 用户级集成
<div class="title-en">User Scope Integration</div>

用户级安装适合个人跨项目复用：

```bash
worktrail install cursor --user
worktrail install codex --user
worktrail install claude --user
```

默认未指定 scope 时，`worktrail install <tool>` 等价于用户级安装。

用户级安装会写入跟随个人环境的规则和 skills。Cursor 用户级安装会安装 Cursor 可见的 Worktrail rule 和 skills；如果已有兼容的 `$HOME/.agents/skills`、`$HOME/.codex/skills` 或 `$HOME/.claude/skills`，Cursor 可以复用这些可见 skills。

### 项目级集成
<div class="title-en">Project Scope Integration</div>

项目级安装适合让当前仓库带上运行时集成配置：

```bash
worktrail install cursor --project
worktrail install codex --project
worktrail install claude --project
```

项目级安装主要写入当前仓库需要的 hooks、settings 配置和 `.gitignore` 运行时条目。它不会从 `templates/root/**` 或 `templates/skills/**` 安装项目级规则或项目级 skills。

### 一次安装用户级和项目级
<div class="title-en">Install Both Scopes</div>

如果你想完整接入某个工具：

```bash
worktrail install cursor --user --project
```

如果你要同时接入 Codex、Claude Code 和 Cursor：

```bash
worktrail install all --user --project
```

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
```

检查知识治理问题：

```bash
worktrail doctor knowledge
```

## 卸载集成
<div class="title-en">Uninstall Integrations</div>

如果需要移除某个 Agent 集成：

```bash
worktrail uninstall cursor --user
worktrail uninstall cursor --project
```

卸载集成只处理对应集成写入的托管文件，不等同于删除 `.worktrail/` 中的知识、状态或候选记录。

## 安装后的第一轮验证
<div class="title-en">First Validation</div>

安装完成后建议做三件事：

- 运行 `worktrail context "test task"`，确认能生成上下文包
- 运行对应 `worktrail doctor <tool> --user|--project`，确认集成文件存在
- 在真实小任务里观察 Agent 是否会在开始、长任务中和结束前使用 Worktrail skills 或等价命令

如果第一步就失败，先看[故障排查](TROUBLESHOOTING.md)。
