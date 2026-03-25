# SciCLI Agent 架构说明

## 1. 项目定位

SciCLI 不是单纯的终端聊天壳，而是一个面向代码与科研工作流的本地优先 Agent 系统。

它的核心目标是把下面几件事统一起来：

- 终端交互
- 大模型推理
- 工具调用
- 会话持久化
- 研究状态管理
- 子任务代理执行

可以把它理解为：

```text
Terminal UI / CLI
    -> Agent Runtime
    -> Provider Adapter
    -> Tool System
    -> Session / Message / Research / TaskRun Storage
```

## 2. 总体架构

SciCLI 的运行主链路是：

```text
用户输入
  -> TUI / CLI
  -> App 运行时容器
  -> Coder Agent
  -> Provider
  -> Tool Calls
  -> Message / Task / Research 持久化
  -> UI 订阅事件并刷新
```

关键分层如下：

### 2.1 入口层

- `main.go`
- `cmd/`

职责：

- 解析命令行参数
- 加载配置
- 执行 onboarding
- 建立数据库连接
- 决定进入 TUI 模式还是非交互模式

### 2.2 运行时容器层

- `internal/app/app.go`

这是整个系统的装配中心。`App` 会统一初始化并持有：

- `session.Service`
- `message.Service`
- `history.Service`
- `research.Service`
- `taskrun.Service`
- `permission.Service`
- `skills.Service`
- LSP clients
- `CoderAgent`

因此 `App` 本质上是 SciCLI 的运行时上下文。

### 2.3 Agent 层

- `internal/llm/agent/`
- `internal/llm/provider/`
- `internal/llm/tools/`
- `internal/llm/models/`
- `internal/llm/prompt/`

这是系统最核心的一层，负责把“用户意图”变成“可执行的模型-工具循环”。

### 2.4 交互层

- `internal/tui/`
- `internal/tui/page/`
- `internal/tui/components/`

职责：

- 展示会话
- 展示工具调用和结果
- 接收用户输入
- 展示权限请求、日志、任务、研究状态

这一层不直接承载核心业务逻辑，更多是事件驱动的视图层。

### 2.5 持久化层

- `internal/db/`
- `internal/session/`
- `internal/message/`
- `internal/history/`
- `internal/research/`
- `internal/taskrun/`

职责：

- 保存会话和消息
- 保存工具/任务时间线
- 保存研究目标、实验计划和评估结果
- 支撑长会话恢复和多轮工作流延续

## 3. Agent 核心架构

## 3.1 Agent 的职责边界

SciCLI 的 Agent 并不只是“调一次模型 API”。

它负责完整执行以下流程：

1. 读取当前 session 上下文
2. 构造 system prompt 与消息历史
3. 按配置选择模型 provider
4. 注册工具集合
5. 发起模型流式请求
6. 解析增量输出与工具调用
7. 执行工具并写回结果
8. 持续循环直到结束、失败或取消
9. 记录 finish reason、任务状态和错误信息

对应核心文件：

- `internal/llm/agent/agent.go`

## 3.2 Agent 运行模型

SciCLI 的 Agent 是典型的 ReAct / tool-using agent 结构，但做了工程化拆分。

它的基本循环可以表示为：

```text
User Prompt
  -> Agent 组装上下文
  -> Provider 输出文本或 Tool Call
  -> Tool 执行
  -> Tool Result 写回消息流
  -> 再次调用 Provider
  -> 直到 EndTurn / Error / Cancel
```

这里有几个关键点：

- Agent 本身不绑定具体模型厂商
- Agent 只依赖统一的 `Provider` 接口
- 工具也是统一抽象，Agent 不关心工具内部细节
- 消息和任务状态每一步都可持久化

## 3.3 Provider 抽象

Provider 层的目标是屏蔽不同模型后端的差异。

核心目录：

- `internal/llm/provider/`
- `internal/llm/models/`

当前设计思路是：

- `models/` 负责描述模型元数据
- `provider/` 负责实际请求与流式处理

这样做的好处是：

- 模型选择和传输实现解耦
- 新 provider 接入成本更低
- OpenAI、Gemini、OpenRouter、xAI、OpenAI-compatible 等都能纳入统一运行时

## 3.4 Tool 架构

Tool 是 SciCLI 和普通聊天客户端的核心差异之一。

核心目录：

- `internal/llm/tools/`

内置工具包括：

- shell / bash
- 文件查看与编辑
- patch
- grep / glob / ls
- diagnostics
- fetch
- 子 Agent
- skill 激活

Agent 不直接内嵌工具逻辑，而是把工具注册给模型，再在 tool call 发生时调度执行。

这种设计的意义是：

- 工具集可扩展
- 工具调用可追踪
- 工具结果可压缩、可回放、可审计

## 3.5 会话与消息模型

Agent 之所以能长时间工作，依赖于稳定的消息持久化层。

相关模块：

- `internal/session/`
- `internal/message/`

这里保存的不只是用户和助手文本，还包括：

- tool calls
- tool results
- finish reason
- 流式生成后的最终消息状态

这使得 SciCLI 能做到：

- 会话恢复
- 长任务观察
- 错误回溯
- 工具链复盘

## 3.6 TaskRun 子任务架构

相关模块：

- `internal/taskrun/`

这是 SciCLI 相比普通单线程 agent 更重要的增强点。

它用来记录和管理子任务执行，包括：

- 父子 session 关系
- 任务状态
- 进度事件
- 取消行为
- 时间线回放

这让 SciCLI 可以把复杂任务拆成多个可观察、可追踪的运行单元，而不是把所有过程都塞进一条聊天记录里。

## 3.7 Research 状态架构

相关模块：

- `internal/research/`

这部分是 SciCLI 从代码代理向科研工作台演进的关键设计。

它没有把“研究目标、实验、评估”只留在自然语言上下文里，而是显式建模了：

- objective
- hypothesis
- experiment
- evaluation
- artifact

价值在于：

- 研究状态可以独立于聊天记录保存
- 可以恢复工作上下文
- 可以对实验候选、晋级和评估做结构化管理

## 4. 交互与事件机制

TUI 不是直接轮询状态，而是通过事件订阅驱动刷新。

相关模块：

- `internal/pubsub/`
- `internal/tui/`

整体思路：

- Agent、TaskRun、Research 等服务发布事件
- TUI 订阅这些事件
- UI 根据事件更新会话区、日志区、inspector、research workbench

这套机制的优点是：

- UI 与核心逻辑解耦
- 流式输出体验更自然
- 任务、权限、消息可并行更新

## 5. 关键技术改进

SciCLI 相比原始 baseline 的技术改进，主要集中在 Agent 工程化能力上。

### 5.1 从“聊天”升级到“可执行 Agent”

不再只是模型回复文本，而是具备：

- 工具调用
- 文件操作
- shell 执行
- diagnostics
- 子任务代理

这让 SciCLI 成为真正的工作代理，而不是问答工具。

### 5.2 从“单轮上下文”升级到“持久化工作流”

通过 Session + Message + TaskRun + Research 的组合，SciCLI 可以保存：

- 会话轨迹
- 工具轨迹
- 子任务轨迹
- 研究轨迹

这使系统可以承载长周期工作，而不是一次性问答。

### 5.3 Provider 抽象增强

原始代码代理往往强绑定单一模型供应商，而 SciCLI 做了 provider 抽象层，支持：

- 多模型路由
- 自定义 OpenAI-compatible endpoint
- 在不改 Agent 主循环的前提下替换后端

这对工程落地非常关键。

### 5.4 Tool 输出控制与上下文保护

SciCLI 明确考虑了上下文窗口压力，做了两类重要改进：

- 长工具输出压缩
- 长对话自动 compact/summarize

这类改进直接提升了 Agent 在真实工程项目中的稳定性。

### 5.5 可观测性增强

SciCLI 对 Agent 运行过程做了更强的可见化处理：

- finish reason
- task timeline
- logs
- permission status
- inspector

这意味着用户可以看到 Agent 为什么停、卡在哪、失败在哪，而不是面对一个黑盒。

### 5.6 科研工作流增强

这是 SciCLI 最有区分度的演进之一：

- 将 objective / experiment / evaluation 结构化
- 把 artifact 与 task run 关联
- 在 TUI 中展示 research workbench

这让 SciCLI 不只适合代码修复，也适合科研探索、实验迭代和结果比较。

## 6. 设计思路总结

SciCLI 的设计重点不是“做一个更漂亮的终端聊天 UI”，而是把 Agent 做成一个真正可运行、可持续、可追踪的本地工作系统。

它的核心思路可以概括为：

### 6.1 用 App 做运行时装配

把会话、消息、任务、研究、权限、技能、LSP 都挂到统一运行时容器上，避免模块之间散乱耦合。

### 6.2 用 Agent 做执行中枢

把 prompt、provider、tool、stream、error、cancel、finish reason 都收束到一条主循环中。

### 6.3 用结构化状态替代纯文本记忆

研究目标、实验计划、任务时间线都不能只留在上下文文本里，必须有结构化状态。

### 6.4 用事件驱动 UI，而不是 UI 驱动系统

UI 负责展示和操作，运行状态由服务层发布，避免把业务逻辑塞进界面组件。

## 7. 一句话总结

SciCLI 的本质，是一个基于 Go 构建的终端 Agent Runtime：

- 上层是 CLI/TUI 交互
- 中层是可扩展的 Agent + Provider + Tool 主循环
- 下层是 Session / Message / TaskRun / Research 持久化

它最重要的技术价值，不在于“接了多少模型”，而在于把 Agent 从一次性聊天，做成了可执行、可恢复、可追踪、可扩展的工程系统。
