# Worktrail 使用手册
<div class="title-en">Worktrail Usage Manual</div>

这是一套面向最终使用者的手册，用来说明如何把 `worktrail` 用到日常 AI 编程会话中。

这套手册优先回答下面几个问题：

- `worktrail` 是什么，适合解决什么问题
- 第一次应该怎样初始化和验证
- 如何选择用户级安装、项目级安装和 Agent 集成
- 如何把上下文、状态、候选知识、评审和维护串成日常工作流
- 遇到常见问题时应该怎样排查

为避免同一件事在多处重复展开，这套手册按几个层次拆开：

- [快速开始](QUICK-START.md) / [安装说明](INSTALLATION.md): 先解决怎么跑通、怎么接入 Agent
- [设计理念](DESIGN-PHILOSOPHY.md): 再解释 Worktrail 为什么这样设计
- [自动化](AUTOMATION.md): 再解释自动化由什么触发、自动到哪里为止
- [常见工作流](COMMON-WORKFLOWS.md): 再落到真实任务里应该怎样使用
- [故障排查](TROUBLESHOOTING.md) / [常见问题](FAQ.md): 最后收口排错和高频问题

## 本地预览
<div class="title-en">Local Preview</div>

这套手册可以单独用 Docsify 本地预览，不需要启动整个项目：

```bash
make docs-manual-serve
```

这个命令会通过 `npx docsify-cli` 启动预览，所以本机需要可用的 Node.js/npm。第一次运行时，`npx` 可能需要下载 `docsify-cli`。

默认端口是 `3000`。如果你想改端口：

```bash
make docs-manual-serve PORT=3001
```

这和 `worktrail preview` 不是同一件事：

- `make docs-manual-serve` 预览的是这套使用手册站点
- `worktrail preview` 预览的是当前 scope 下的 Worktrail 知识库静态多页站点入口页

## 推荐阅读顺序
<div class="title-en">Recommended Reading Order</div>

1. [快速开始](QUICK-START.md)
2. [设计理念](DESIGN-PHILOSOPHY.md)
3. [自动化](AUTOMATION.md)
4. [安装说明](INSTALLATION.md)
5. [常见工作流](COMMON-WORKFLOWS.md)
6. [故障排查](TROUBLESHOOTING.md)
7. [常见问题](FAQ.md)

## 按目标阅读
<div class="title-en">Recommended Paths by Goal</div>

- 我想先跑通一次： [快速开始](QUICK-START.md) -> [安装说明](INSTALLATION.md) -> [故障排查](TROUBLESHOOTING.md)
- 我想先理解 Worktrail 为什么这样设计： [设计理念](DESIGN-PHILOSOPHY.md) -> [安装说明](INSTALLATION.md) -> [常见工作流](COMMON-WORKFLOWS.md)
- 我想知道 Worktrail 的自动化做了什么、没做什么： [自动化](AUTOMATION.md) -> [安装说明](INSTALLATION.md) -> [常见工作流](COMMON-WORKFLOWS.md)
- 我想把 Worktrail 接入 Codex、Claude Code、Cursor 或 ZCode Agent： [安装说明](INSTALLATION.md) -> [常见工作流](COMMON-WORKFLOWS.md) -> [常见问题](FAQ.md)
- 我想在任务开始前给 Agent 上下文： [快速开始](QUICK-START.md) -> [常见工作流](COMMON-WORKFLOWS.md)
- 我想把会话里的结论沉淀成知识： [常见工作流](COMMON-WORKFLOWS.md) -> [故障排查](TROUBLESHOOTING.md)
- 我已经有 pending candidates，需要 review 或维护： [常见工作流](COMMON-WORKFLOWS.md) -> [常见问题](FAQ.md)

如果你不确定自己属于哪一类，默认从[快速开始](QUICK-START.md)开始。

## 章节导览
<div class="title-en">Chapter Guide</div>

- [快速开始](QUICK-START.md): 用最短路径初始化并读取一次上下文
- [设计理念](DESIGN-PHILOSOPHY.md): 解释 local-first、chat-native review 和知识分层背后的原因
- [自动化](AUTOMATION.md): 解释 hooks、skills、maintenance hints，以及 ZCode Agent 的语义自动化边界
- [安装说明](INSTALLATION.md): 说明 CLI、用户级/项目级 scope 和 Agent 集成
- [常见工作流](COMMON-WORKFLOWS.md): 给出任务前、任务中、任务后、知识维护的典型流程
- [故障排查](TROUBLESHOOTING.md): 汇总安装、scope、候选知识、预览和导入问题
- [常见问题](FAQ.md): 汇总高频使用问题和快速答案

## 范围说明
<div class="title-en">Scope</div>

这个目录面向两类读者：

- 想在自己机器上跨项目使用 Worktrail 的个人使用者
- 想在团队项目中共享 Agent 上下文、规则、状态和知识维护流程的项目使用者

以下内容不作为本目录重点：

- Worktrail 内部实现细节
- 发布验收和 dogfood 验证记录
- 测试夹具和开发者专用文档
- 低层 JSON schema 的完整字段说明
