# MewCode 项目

## 项目简介

MewCode 是一个用 Go 编写的终端 AI 编程助手（Coding Agent），类似 Claude Code / OpenCode。它在终端中运行，通过 LLM 驱动一个具备工具调用能力的 Agent 循环，能够读写文件、执行命令、搜索代码、管理 git worktree，并支持多智能体协作、MCP 工具扩展、Skills 技能系统和长期记忆。

## 核心特性

- **多 Provider 支持**：Anthropic（Claude）、OpenAI（Responses API）、OpenAI 兼容协议（Chat Completions API，适配 DeepSeek 等）
- **工具系统**：内置 Bash、ReadFile、WriteFile、EditFile、Glob、Grep 等工具，支持文件状态缓存和延迟加载
- **MCP 集成**：支持 stdio 和 HTTP/SSE 两种传输方式连接外部 MCP Server，自动将 MCP 工具注册为 Agent 可用工具
- **Skills 技能系统**：通过 SKILL.md 定义可复用技能，支持 inline（注入对话）和 fork（子 Agent 隔离执行）两种模式
- **Teams 多智能体**：支持 in-process / tmux / iTerm 三种后端，成员间通过文件邮箱通信，支持 Coordinator 协调模式
- **记忆系统**：双层记忆目录（用户级 + 项目级），自动提取和整合记忆，支持相关性召回
- **Context 管理**：双层上下文压缩 —— Layer 1 工具结果溢出裁剪，Layer 2 LLM 驱动的全对话摘要
- **权限系统**：default / acceptEdits / plan / bypassPermissions 四种模式，按工具类别（read/write/command）控制
- **沙箱**：macOS seatbelt / Linux bubblewrap 的 OS 级沙箱，控制文件读写和网络访问
- **Git Worktree**：在 `.mewcode/worktrees/` 下自动创建隔离工作区，支持子 Agent 隔离开发
- **会话管理**：JSONL 格式持久化会话，支持 resume 和压缩边界记录
- **Hooks 钩子**：支持 command / prompt / http / agent 四种动作，覆盖会话和工具使用的全生命周期
- **远程模式**：通过 `--remote` 启动 WebSocket 服务，配合 Web UI 远程使用
- **TUI 交互**：基于 bubbletea 的终端界面，支持 Markdown 渲染、流式输出、斜杠命令

## 架构设计

### 入口与启动流程

`cmd/mewcode/main.go` 是程序入口，启动时按以下顺序处理：

1. 解析 teammate 子命令标志（子 Agent 模式）
2. 解析 `--remote` 标志（远程 WebSocket 模式）
3. 加载配置（`internal/config`）—— 按优先级合并 `~/.mewcode/config.yaml` → `<project>/.mewcode/config.yaml` → `<project>/.mewcode/config.local.yaml`
4. 校验 Hooks 配置
5. 解析 `-p/--print` 标志（非交互式 print 模式）
6. 根据模式启动：远程服务（`internal/remote`）或 TUI 交互（`internal/tui`）

### 内部包职责

| 包 | 职责 |
|---|---|
| `internal/agent` | 核心 Agent 循环，协调 LLM 调用、工具执行、上下文压缩、记忆注入 |
| `internal/agents` | 子 Agent 定义与加载，支持 worktree/remote 隔离模式，工具过滤 |
| `internal/commands` | 斜杠命令系统（`/compact`、`/memory`、`/skills` 等），支持 local / local-ui / prompt / skill-fork 四种类型 |
| `internal/compact` | Layer 2 上下文压缩：LLM 驱动的全对话摘要，token 占用超 80% 时自动触发 |
| `internal/config` | 配置加载与合并，Provider/MCP/Hooks/Sandbox 配置结构体定义 |
| `internal/conversation` | 对话消息管理，维护消息历史和 thinking blocks |
| `internal/filehistory` | 文件操作历史记录，用于 diff 和回溯 |
| `internal/history` | 命令历史管理 |
| `internal/hooks` | Hooks 引擎，支持 session/turn/tool 生命周期事件，command/prompt/http/agent 动作 |
| `internal/llm` | LLM 客户端抽象层，三种实现：`anthropic`、`openai`（Responses API）、`openai-compat`（Chat Completions API） |
| `internal/mcp` | MCP Server 管理，stdio/HTTP/SSE 传输，工具自动注册 |
| `internal/memory` | 双层记忆系统，用户级（`~/.mewcode/memory/`）+ 项目级（`.mewcode/memory/`），含相关性召回和自动整合 |
| `internal/permissions` | 权限检查器，按工具类别和模式矩阵决策 allow/deny/ask |
| `internal/planfile` | Plan 模式的计划文件管理 |
| `internal/prompt` | 系统提示词构建，包含 skills、memory、plan 等段落拼装 |
| `internal/remote` | 远程 WebSocket 服务，Web UI 后端 |
| `internal/sandbox` | OS 级沙箱抽象，macOS seatbelt / Linux bubblewrap / 其他平台 noop |
| `internal/session` | JSONL 会话持久化，支持 resume 和压缩边界（compact_boundary）记录 |
| `internal/skills` | Skills 加载/解析/执行，frontmatter 元数据 + Markdown body，支持 inline/fork 模式 |
| `internal/teams` | 多智能体团队，in-process/tmux/iTerm 后端，文件邮箱通信，Coordinator 模式 |
| `internal/todo` | TODO 列表管理工具，供 Agent 跟踪多步骤任务 |
| `internal/toolresult` | Layer 1 上下文管理：工具结果溢出裁剪、内容替换状态、预算控制 |
| `internal/tools` | 内置工具实现和注册表，工具接口定义（read/write/command 三类） |
| `internal/tui` | 终端 UI，基于 bubbletea + glamour，处理输入、渲染、斜杠命令分发 |
| `internal/worktree` | Git worktree 创建/管理/清理，子 Agent 隔离工作区 |

## 支持的 LLM Provider

| 协议 | base_url 示例 | 说明 |
|---|---|---|
| `anthropic` | `https://api.anthropic.com` | 原生 Anthropic Messages API，支持 extended thinking |
| `openai` | `https://api.openai.com/v1` | OpenAI Responses API（`/responses` 端点） |
| `openai-compat` | `https://api.deepseek.com/v1` | OpenAI 兼容的 Chat Completions API（`/chat/completions`），适配 DeepSeek 等第三方 |

## 配置说明

配置文件路径：`~/.mewcode/config.yaml`（全局）或 `<project>/.mewcode/config.yaml`（项目级），按优先级合并。

```yaml
providers:
  - name: anthropic-official
    protocol: anthropic              # anthropic / openai / openai-compat
    base_url: https://api.anthropic.com
    api_key: "your-api-key"
    model: claude-sonnet-4-20250514
    thinking: true                   # 是否开启 extended thinking
    context_window: 200000           # 可选，覆盖自动推断的上下文窗口
    max_output_tokens: 64000         # 可选，覆盖默认最大输出

permission_mode: default             # default / acceptEdits / plan / bypassPermissions

mcp_servers:
  - name: context7
    command: npx
    args: ["-y", "@upstash/context7-mcp"]
  # HTTP MCP 示例
  # - name: my-http-mcp
  #   url: https://example.com/mcp
  #   transport: http                # http（默认）/ sse
  #   headers:
  #     Authorization: "Bearer ${MY_TOKEN}"

enable_coordinator_mode: false       # true 后 Team 的 Lead 只能调度不能写代码
```

## 工具系统

内置工具按类别分为：

- **Read**：`read_file`、`glob`、`grep`、`tool_search`（延迟加载工具搜索）
- **Write**：`write_file`、`edit_file`
- **Command**：`bash`

工具通过 `Registry` 统一注册，Schema 按协议自动适配（Anthropic 用 `input_schema`，OpenAI 用 `parameters`）。支持延迟加载（DeferrableTool），未发现的工具不发送 Schema，按需通过 `tool_search` 发现。

## Skills 系统

Skills 存放在 `.mewcode/skills/` 下，每个 Skill 是一个目录，包含 `SKILL.md`（frontmatter + Markdown body）。支持两种执行模式：

- **inline**（默认）：将 Skill body 注入当前对话
- **fork**：在隔离的子 Agent 中执行，支持 `fork_context` 控制父对话上下文传递量（full / recent / none）

## Teams 多智能体

通过 `/team` 命令创建多智能体团队，支持三种后端：

- **in-process**：同一进程内 goroutine 协作
- **tmux**：tmux 窗格隔离
- **iTerm**：iTerm 标签页隔离

成员间通过文件邮箱（`.mewcode/teams/<name>/inboxes/`）异步通信。Coordinator 模式下 Lead 只能调度任务不能直接写代码。

## Memory 记忆系统

双层记忆目录：

- 用户级：`~/.mewcode/memory/`（用户偏好、反馈）
- 项目级：`<project>/.mewcode/memory/`（项目上下文、参考信息）

支持自动提取（`internal/memory/extractor`）和整合（`internal/memory/consolidation`），对话结束时后台提取记忆，对话开始时按相关性召回注入系统提示词。

## Worktree 支持

在 `.mewcode/worktrees/` 下自动创建 git worktree，为子 Agent 或 team 成员提供隔离的工作目录。支持自动创建分支、post-creation setup、环境变量传递和清理。

## 技术栈

- **语言**：Go 1.25
- **TUI 框架**：charmbracelet/bubbletea + bubbles + lipgloss + glamour（Markdown 渲染）
- **Anthropic SDK**：github.com/anthropics/anthropic-sdk-go
- **OpenAI SDK**：github.com/openai/openai-go
- **MCP SDK**：github.com/modelcontextprotocol/go-sdk
- **配置**：gopkg.in/yaml.v3
- **WebSocket**：github.com/gorilla/websocket（远程模式）

## 代码规范

- commit message 用英文
- 变量命名用 snake_case
