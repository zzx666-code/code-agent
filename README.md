# MewCode

MewCode 是一个用 Go 编写的终端 AI 编程助手，支持多 LLM Provider、MCP 工具扩展、Skills 技能系统、多智能体协作和长期记忆。

## 目录结构

```
mewcode/
├── cmd/
│   └── mewcode/
│       ├── main.go              # 程序入口
│       ├── print.go             # -p/--print 非交互模式
│       └── teammate.go          # teammate 子命令（子 Agent 模式）
├── internal/
│   ├── agent/                   # 核心 Agent 循环
│   │   ├── agent.go
│   │   ├── events.go
│   │   └── streaming_executor.go
│   ├── agents/                  # 子 Agent 定义与加载
│   │   ├── definition.go
│   │   ├── loader.go
│   │   ├── subagent.go
│   │   ├── tool_filter.go
│   │   └── agent_tool.go
│   ├── commands/                # 斜杠命令系统
│   │   ├── commands.go
│   │   ├── loader.go
│   │   └── sandbox.go
│   ├── compact/                 # Layer 2 上下文压缩
│   │   ├── compact.go
│   │   └── recovery.go
│   ├── config/                  # 配置加载与合并
│   │   └── config.go
│   ├── conversation/            # 对话消息管理
│   │   └── conversation.go
│   ├── filehistory/             # 文件操作历史
│   │   └── filehistory.go
│   ├── history/                 # 命令历史
│   │   └── history.go
│   ├── hooks/                   # Hooks 钩子引擎
│   │   └── hooks.go
│   ├── llm/                     # LLM 客户端抽象层
│   │   ├── client.go            #   Client 接口与工厂
│   │   ├── anthropic.go         #   Anthropic Messages API
│   │   ├── openai.go            #   OpenAI Responses API
│   │   ├── openai_compat.go     #   OpenAI 兼容 Chat Completions API
│   │   ├── model_resolver.go
│   │   ├── errors.go
│   │   └── events.go
│   ├── mcp/                     # MCP Server 管理
│   │   └── mcp.go
│   ├── memory/                  # 记忆系统
│   │   ├── memory.go
│   │   ├── memory_types.go
│   │   ├── memory_scan.go
│   │   ├── memory_age.go
│   │   ├── find_relevant_memories.go
│   │   ├── instructions.go
│   │   ├── paths.go
│   │   ├── memdir.go
│   │   ├── extractor/           #   记忆自动提取
│   │   └── consolidation/       #   记忆整合
│   ├── permissions/             # 权限检查
│   │   └── permissions.go
│   ├── planfile/                # Plan 模式
│   │   └── planfile.go
│   ├── prompt/                  # 系统提示词构建
│   │   ├── builder.go
│   │   ├── sections.go
│   │   └── plan_mode.go
│   ├── remote/                  # 远程 WebSocket 服务
│   │   ├── server.go
│   │   └── web.go
│   ├── sandbox/                 # OS 级沙箱
│   │   ├── sandbox.go
│   │   ├── sandbox_darwin.go
│   │   ├── sandbox_linux.go
│   │   └── sandbox_other.go
│   ├── session/                 # 会话持久化
│   │   └── session.go
│   ├── skills/                  # Skills 技能系统
│   │   ├── skills.go
│   │   ├── catalog.go
│   │   ├── parser.go
│   │   ├── executor.go
│   │   ├── install.go
│   │   ├── builtins.go
│   │   ├── load_skill_tool.go
│   │   └── install_tool.go
│   ├── teams/                   # 多智能体团队
│   │   ├── teams.go
│   │   ├── runner.go
│   │   ├── coordinator.go
│   │   ├── spawn.go
│   │   ├── inprocess.go
│   │   ├── tmux.go
│   │   ├── iterm.go
│   │   ├── filemailbox.go
│   │   ├── tools.go
│   │   ├── progress.go
│   │   ├── transcript.go
│   │   └── backend.go
│   ├── todo/                    # TODO 列表管理
│   │   ├── todo.go
│   │   ├── store.go
│   │   └── tools.go
│   ├── toolresult/              # Layer 1 工具结果管理
│   │   ├── budget.go
│   │   ├── reconstruct.go
│   │   ├── record.go
│   │   └── state.go
│   ├── tools/                   # 内置工具
│   │   ├── tool.go              #   工具接口与注册表
│   │   ├── bash.go
│   │   ├── read_file.go
│   │   ├── write_file.go
│   │   ├── edit_file.go
│   │   ├── glob.go
│   │   ├── grep.go
│   │   ├── diff.go
│   │   ├── ask_user.go
│   │   ├── tool_search.go
│   │   ├── media_input.go
│   │   ├── enter_worktree.go
│   │   ├── exit_worktree.go
│   │   ├── exit_plan_mode.go
│   │   ├── file_state_cache.go
│   │   └── descriptions.go
│   ├── tui/                     # 终端 UI
│   │   ├── tui.go
│   │   ├── styles.go
│   │   └── verbs.go
│   └── worktree/                # Git worktree 管理
│       ├── create.go
│       ├── setup.go
│       ├── changes.go
│       ├── cleanup.go
│       ├── session.go
│       ├── agent.go
│       ├── env.go
│       ├── filesystem.go
│       ├── validate.go
│       └── notice.go
├── .mewcode/                    # 运行时目录（配置、会话、记忆、技能）
│   ├── config.yaml
│   ├── config.yaml.example
│   ├── sessions/
│   ├── skills/
│   ├── memory/
│   └── teams/
├── go.mod
├── go.sum
├── MEWCODE.md
└── README.md
```

## 快速开始

### 1. 编译

```bash
go build -o mewcode ./cmd/mewcode
```

### 2. 配置

在项目根目录创建 `.mewcode/config.yaml`（参考 `.mewcode/config.yaml.example`）：

```yaml
providers:
  - name: anthropic-official
    protocol: anthropic
    base_url: https://api.anthropic.com
    api_key: "your-api-key"
    model: claude-sonnet-4-20250514
    thinking: true

mcp_servers:
  - name: context7
    command: npx
    args: ["-y", "@upstash/context7-mcp"]
```

### 3. 运行

```bash
# 交互式 TUI
./mewcode

# 非交互模式
./mewcode -p "解释这个项目"

# 远程模式（启动 WebSocket 服务）
./mewcode --remote :18888
```

我是小喵
