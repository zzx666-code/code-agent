# AGENTS.md

## Project Overview

mewcode is a command-line AI coding agent written in Go (1.25.0), architecturally similar to Claude Code / opencode. It drives an LLM through a tool-calling loop (read -> edit -> run -> verify) with layered context management, permission-gated tool execution, pluggable MCP servers, and multi-agent team coordination.

## Architecture

The codebase lives under `internal/` and is split into focused packages:

| Package | Responsibility |
|---|---|
| `agent` | Core agent loop (`Agent.Run`), streaming tool executor, LLM turn orchestration, verification gate, recovery state |
| `agents` | Sub-agent definitions, loader, spawn (sync/async/fork), task manager, verification agent spec, VERDICT parser |
| `commands` | Slash-command registry (`/clear`, `/resume`, `/skills`, etc.) |
| `compact` | Layer-2 auto-compaction: threshold detection, summary generation, recovery snapshot, circuit breaker |
| `config` | `.mewcode/config.yaml` loading and provider/MCP wiring |
| `conversation` | In-memory message history (`Manager`), tool-use/result blocks, thinking blocks |
| `filehistory` | Per-session file backup snapshots for rewind; `TrackEdit` before writes, `MakeSnapshot` at task boundaries, `Rewind` for rollback |
| `history` | Prompt input history (up-arrow recall) persisted to `.mewcode/prompt_history.jsonl` |
| `hooks` | Lifecycle hook engine (session start/end, turn start/end, pre/post send) |
| `llm` | Provider-agnostic client interface, Anthropic/OpenAI/OpenAI-compat implementations, typed errors, LLM-as-judge |
| `mcp` | Model Context Protocol server lifecycle and tool bridge |
| `memory` | Persistent memory (project + user), instruction loading, recall/extract/consolidation |
| `permissions` | 5-layer permission checker: plan exception -> safe commands -> dangerous detection -> path sandbox -> rule engine -> session allow-always |
| `planfile` | Plan-mode file management (`.mewcode/plans/<slug>.md`) |
| `prompt` | System prompt section builder (12 prioritized sections: Identity, System, DoingTasks, ExecutingActions, UsingTools, ToneStyle, OutputEfficiency, Environment, RAG, CustomInstructions, Skills, Memory) |
| `rag` | Retrieval-augmented generation: embedding store, chunking, reranking, recall |
| `remote` | WebSocket remote server (headless agent over the wire) |
| `sandbox` | OS-level command sandbox (bwrap on Linux, seatbelt on macOS, none on Windows) |
| `session` | Append-only JSONL session log, compact boundary persistence, resume |
| `skills` | Skill host and loader (inline + fork modes), `SKILL.md` frontmatter parsing |
| `teams` | Multi-agent coordination: in-process/tmux/iTerm backends, file mailbox, coordinator mode |
| `todo` | Task list (`.mewcode/tasks/<session>.json`) with blocks/blockedBy (advisory, no topological enforcement) |
| `toolresult` | Layer-1 tool-result budget management: overflow spilling, frozen decisions, prompt-cache prefix stability |
| `tools` | Tool registry, built-in tools (Bash, ReadFile, WriteFile, EditFile, Glob, Grep, Agent, etc.), deferred tool loading |
| `tui` | Terminal UI (bubbletea), session orchestration, permission prompts, memory wiring |
| `worktree` | Git worktree isolation for sub-agents |

## Coding Standards

- **Language**: Go 1.25.0, module `mewcode`
- **Commit messages**: English
- **Variable naming**: snake_case (project convention; note this overrides Go's camelCase default)
- **Comments**: do not add comments unless necessary
- **Files**: prefer editing existing files over creating new ones; never proactively create documentation files
- **Emojis**: do not use emojis in code or commit messages

## Pull Request Guidelines

### Branch naming
`<type>/<short-description>` in kebab-case, where type is one of:
- `feat` - new feature
- `fix` - bug fix
- `refactor` - code restructuring without behavior change
- `chore` - build, deps, config, tooling
- `test` - test additions or fixes
- `docs` - documentation only

Example: `feat/verification-gate`

### Commit / PR title format
Conventional commits style:
```
feat: <imperative summary>
fix: <imperative summary>
refactor: <imperative summary>
chore: <imperative summary>
test: <imperative summary>
docs: <imperative summary>
```
Keep the summary under 72 characters, lowercase, no trailing period.

### PR description template
```
## What changed
<1-3 sentences>

## Why
<motivation; reference issue if any>

## How to test
<commands or steps to verify>

## Breaking changes
<none, or describe>

## Checklist
- [ ] go build ./... passes
- [ ] go test ./... passes
- [ ] go vet ./... clean
- [ ] no secrets committed
- [ ] no .mewcode/ runtime data committed (except config.yaml.example)
- [ ] tests included with feature changes
```

### Rules
- One PR = one concern. Do not mix features with refactors.
- Tests must ship with the feature they cover.
- Do not commit secrets or API keys.
- Do not commit `.mewcode/` runtime data (sessions, tasks, plans, memory, file-history) except `config.yaml.example`.
- Do not amend or force-push unless explicitly requested.

## Testing

- Test files: `*_test.go` alongside the code under test
- Test functions: `TestXxx(t *testing.T)` using the standard `testing` package
- Benchmark functions: `BenchmarkXxx(b *testing.B)` in `*_bench_test.go`
- E2E / eval tests gated by build tags (`//go:build e2e`)
- Run: `go test ./...`
- Race detector: `go test -race ./...`
- Benchmarks: `go test -bench=. -benchmem ./internal/...`

## Configuration

Runtime config lives under `.mewcode/` (gitignored except `config.yaml.example`):

| Path | Purpose |
|---|---|
| `config.yaml` | Providers, MCP servers, `permission_mode`, `enable_coordinator_mode`, `enable_verification` |
| `permissions.local.yaml` | Tool-level allow/deny/ask rules (`effect` + `rule: ToolName(pattern)`) |
| `skills/<name>/SKILL.md` | Skill definitions (YAML frontmatter + Markdown SOP) |
| `memory/` | Persistent project memory (Markdown files) |
| `sessions/<id>.jsonl` | Append-only session logs |
| `tasks/<id>.json` | Todo task lists |
| `plans/<slug>.md` | Plan-mode plan files |
| `file-history/<session>/` | File backup snapshots for rewind |
| `agents/<name>.md` | Custom sub-agent definitions |

Instruction files are discovered in order (see `internal/memory/instructions.go`):
1. User global: `~/.mewcode/MEWCODE.md`, `~/.mewcode/AGENTS.md`
2. Project: walk from git root to workDir picking up `MEWCODE.md` and `AGENTS.md` (closest wins)
3. `workDir/.mewcode/INSTRUCTIONS.md` (legacy)
4. `workDir/MEWCODE.local.md` (private local override)

## Agent-Specific Rules

When working in this repository, an AI agent must:

- **Read the main loop before editing it.** `internal/agent/agent.go` `Agent.Run` (line ~161) orchestrates 12 prompt sections, 2-layer compaction, streaming tool execution, and the verification gate. Understand the iteration structure before touching it.
- **Respect the 5-layer permission check.** `internal/permissions/permissions.go` `Checker.Check` runs: plan exception -> safe command -> dangerous detection -> path sandbox -> rule engine -> session allow-always. Do not bypass layers.
- **Route every file write through `filehistory.TrackEdit`.** Any tool that modifies files must call `TrackEdit(path)` before the write so rewind can restore the prior state.
- **Classify LLM errors by type.** `internal/llm/errors.go` defines `AuthenticationError`, `RateLimitError`, `NetworkError`, `ContextTooLongError`, `LLMError`. `handleStreamError` maps each to a retry strategy; new error handling must follow this taxonomy.
- **Keep tool-result decisions frozen.** `internal/toolresult` freezes overflow/spill decisions per `tool_use_id` for prompt-cache prefix stability. Once a result is spilled to disk, the replacement preview must not change on later iterations.
- **Verification gate is the close-loop control.** `Agent.VerificationGate` runs at the completion point; `FAIL` triggers graded recovery (soft retry -> rewind+retry -> rewind+terminate). Do not declare `LoopComplete` without the gate when verification is enabled and the change is non-trivial (3+ file edits).
- **Persist snapshots.** Call `filehistory.Save()` after every `MakeSnapshot` so rewind survives crashes.
- **Do not mutate `conversation.Manager` from another goroutine.** The manager has no mutex; only the agent's main goroutine may append to it.
