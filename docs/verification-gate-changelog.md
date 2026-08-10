# Verification Gate & Graded Recovery - Change Log

## 概述

本次改动为 mewcode 实现了**验评估证闭环**与**失败分级挽回机制**，让系统在任务完成时能自动验证、在验证失败时能分级挽回（软重试 -> 回滚+重试 -> 回滚+终止）。

## 背景

调研发现 mewcode 的 harness 在"驾驭闭环"上存在断裂：

- `verification_prompt.go` 设计了专业的对抗式验证 prompt 和 `VERDICT: PASS/FAIL/PARTIAL` 输出格式，但：
  - verification agent 默认被 feature-gate 关闭（`MEWCODE_VERIFICATION_AGENT=true` 才启用）
  - `VERDICT` 字符串从未被任何代码解析消费
  - 主循环完成点（`len(toolCalls)==0`）无验证拦截，agent 可直接声明完成
- `filehistory.Rewind` 存在但 `Save()` 从不调用，崩溃后回滚失效
- `filehistory.Rewind` 只恢复快照内文件，任务期间新建/编辑的文件无法回滚

## 改动清单

### 新增文件

| 文件 | 作用 |
|---|---|
| `AGENTS.md` | 项目规范（架构、编码标准、PR 规范、测试、配置、agent 规则） |
| `internal/agent/verdict.go` | `Verdict` 枚举 + `ParseVerdict()` 解析 VERDICT 行 + 证据提取 |
| `internal/agent/verdict_test.go` | VERDICT 解析器 9 个测试用例 |
| `internal/agent/verification_gate_test.go` | 验证 gate 7 个测试：PASS 完成、FAIL 重试、FAIL 耗尽、小改动跳过、PARTIAL、shouldVerify、超时 |
| `internal/agent/verification_rewind_test.go` | 分级挽回 4 个测试：软重试不回滚、第2次回滚、新文件删除、集成回滚 |

### 修改文件

| 文件 | 改动 |
|---|---|
| `internal/agent/agent.go` | 加 `maxVerifyRetries=2` 常量；Agent 结构体加 `VerificationGate`/`verifyRetries`/`preTaskSnapshot` 字段；Run() 启动建 pre-task 快照 + `Save()`；完成点插入验证 gate（FAIL 触发分级挽回 `continue`）；新增 `handleVerificationFailure`（第1次软重试，第2次 Rewind+重试）和 `shouldVerify`（3+ 文件编辑才触发）；完成快照后调 `Save()` |
| `internal/agent/events.go` | `LoopComplete` 加 `Verified bool` + `Evidence string`；新增 `VerificationEvent` 事件 |
| `internal/agents/agent_tool.go` | 加 `RunVerification` 导出方法（同步跑 verification sub-agent + 解析 VERDICT）+ `buildVerificationPrompt` 辅助函数 |
| `internal/agents/loader.go` | 去掉 `MEWCODE_VERIFICATION_AGENT` 环境变量门控，改用包级变量 `VerificationEnabled = true`（默认启用） |
| `internal/agents/loader_test.go` | 适配默认启用（期望 4 个 agent）+ 新增 `TestLoaderVerificationDisabled` |
| `internal/config/config.go` | `AppConfig` 加 `EnableVerification *bool` 字段 |
| `internal/filehistory/filehistory.go` | `Rewind` 增强：对不在快照 Backups 里的 tracked 文件，从 version-1 备份恢复（编辑前内容），无备份则删除（新建文件）；重置 trackedFiles 为快照状态 |
| `internal/tui/tui.go` | `installMemoryExtractor` 里接线 `ag.VerificationGate = at.RunVerification` |
| `internal/remote/server.go` | `installMemExtractor` 里同样接线 `ag.VerificationGate` |
| `cmd/mewcode/main.go` | 加载 config 后设置 `agents.VerificationEnabled` |
| `.mewcode/config.yaml.example` | 加 `enable_verification: true` 字段 |

## 验证闭环流程

```
任务完成（len(toolCalls)==0）
    │
    ├─ shouldVerify?（3+ 文件编辑）
    │       └─ 否 ──> LoopComplete(Verified=true)
    │       └─ 是 ──> 同步调 VerificationGate
    │                      │
    │                 PASS/PARTIAL ──> LoopComplete(Verified=true)
    │                      │
    │                 FAIL (retry 0/2)
    │                      ├──> 软重试：注入证据，continue
    │                      │       └─> 再次完成 ──> gate
    │                      │              FAIL (retry 1/2)
    │                      │              ├──> 回滚+重试：Rewind pre-task，注入证据，continue
    │                      │              │       └─> 再次完成 ──> gate
    │                      │              │              FAIL (retry 2/2，耗尽)
    │                      │              │              ├──> LoopComplete(Verified=false)
    │                      │              │              PASS/PARTIAL ──> LoopComplete(Verified=true)
    │                      │              PASS/PARTIAL ──> LoopComplete(Verified=true)
```

## 分级挽回策略

| 重试次数 | 策略 | 行为 |
|---|---|---|
| 第 1 次 FAIL | 软重试 | 不回滚文件，注入失败证据到 conv，agent 在当前文件上修复 |
| 第 2 次 FAIL | 回滚+重试 | `Rewind` 到 pre-task 快照，注入证据，agent 从干净状态重做 |
| 耗尽 (>=2) | 回滚+终止 | `Rewind` 后 `LoopComplete(Verified=false)`，报告失败 |

## 配置

```yaml
# .mewcode/config.yaml
enable_verification: true   # 默认启用；设为 false 禁用验证 gate
```

## 测试验证

```
go test ./internal/agent/ -run "TestParseVerdict|TestVerificationGate|TestHandleVerificationFailure" -v
# 20 个测试全部通过

go test ./internal/agents/ -run "TestLoader" -v
# 4 个测试全部通过

go build ./...   # 通过
go vet ./...     # 通过
```

## 后续改进方向

- verification sub-agent 自身失败（如 build 跑不起来）时 gate 返回 err 不阻断完成，可改为 PARTIAL
- pre-task 快照的 trackedFiles 跨多轮对话累积，可在 LoopComplete 后清理
- 自评机制：把 verification VERDICT 落盘到 `.mewcode/evaluations/<session>.jsonl` 用于事后分析
- CI：加 GitHub Actions 跑 build/test/vet
