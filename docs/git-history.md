# Git History - mewcode

> 项目完整提交历史，按时间正序排列。

## 提交总览

| # | Hash | 日期 | 作者 | 说明 |
|---|---|---|---|---|
| 1 | `a0eba22` | 2026-07-28 | zzx | Initial commit: MewCode terminal AI coding agent in Go |
| 2 | `c04d3f0` | 2026-07-28 | zzx | 实现基础的RAG功能 |
| 3 | `80f433b` | 2026-07-29 | zzx | add feature rag pdf |
| 4 | `d4e8545` | 2026-07-29 | zzx | Add unit tests, integration tests and performance benchmarks for core RAG, agent, tool and compact paths |
| 5 | `e09ce6f` | 2026-07-29 | zzx | Add concurrent race tests for RecoveryState, Store and FileStateCache |
| 6 | `7c32cae` | 2026-07-30 | zzx | Expand RAG eval dataset to 22 docs / 8 topics / 40 queries with difficulty tiers |
| 7 | `c487fd5` | 2026-08-02 | zzx | first commit |

---

## 1. a0eba22 - Initial commit: MewCode terminal AI coding agent in Go

**Date**: 2026-07-28 12:53:19 +0800  |  **Author**: zzx <3354920962@qq.com>

初始提交，建立 mewcode 项目骨架。引入核心 agent 循环、27 个 internal 包、TUI (bubbletea)、LLM 客户端 (Anthropic/OpenAI)、权限系统、工具注册表、会话管理、记忆系统、技能加载器、团队协作等完整架构。

主要文件：
- `cmd/mewcode/main.go` - 入口
- `internal/agent/agent.go` - 主循环 (672 行)
- `internal/agents/agent_tool.go` - 子 agent 工具 (738 行)
- `internal/compact/compact.go` - 两层压缩 (629 行)
- `internal/permissions/` - 5 层权限检查
- `internal/tools/` - 内置工具 (Bash/ReadFile/WriteFile/EditFile/Glob/Grep 等)
- `internal/tui/` - 终端 UI
- `internal/llm/` - LLM 客户端 (Anthropic/OpenAI/OpenAI-compat)
- `internal/memory/` - 持久记忆 + 抽取 + 整理
- `internal/rag/` - RAG 检索增强
- `internal/teams/` - 多 agent 协作

---

## 2. c04d3f0 - 实现基础的RAG功能

**Date**: 2026-07-28 20:45:10 +0800  |  **Author**: zzx <3354920962@qq.com>

实现基础 RAG (检索增强生成) 功能：embedding 存储、文档分块、向量检索、召回。

---

## 3. 80f433b - add feature rag pdf

**Date**: 2026-07-29 11:19:15 +0800  |  **Author**: zzx <3354920962@qq.com>

为 RAG 添加 PDF 文档解析支持，扩展文档类型的覆盖范围。

---

## 4. d4e8545 - Add unit tests, integration tests and performance benchmarks

**Date**: 2026-07-29 15:58:48 +0800  |  **Author**: zzx <3354920962@qq.com>

为核心 RAG、agent、tool 和 compact 路径添加单元测试、集成测试和性能基准测试。大幅提升测试覆盖率。

---

## 5. e09ce6f - Add concurrent race tests

**Date**: 2026-07-29 19:59:38 +0800  |  **Author**: zzx <3354920962@qq.com>

为 RecoveryState、Store 和 FileStateCache 添加并发竞态测试，确保并发安全。

---

## 6. 7c32cae - Expand RAG eval dataset

**Date**: 2026-07-30 14:19:35 +0800  |  **Author**: zzx <3354920962@qq.com>

扩展 RAG 评估数据集至 22 个文档 / 8 个主题 / 40 个查询，含难度分级。为 RAG 检索质量评估 (Recall/Precision/MRR/NDCG) 提供更完整的 ground truth。

---

## 7. c487fd5 - first commit

**Date**: 2026-08-02 22:11:15 +0800  |  **Author**: zzx <3354920962@qq.com>

合并提交。

---

## 未提交的改动（当前工作区）

以下改动已实现但尚未提交（verification gate + 分级挽回机制），详见 `docs/verification-gate-changelog.md`：

- `AGENTS.md` (新增) - 项目规范
- `internal/agent/verdict.go` (新增) - VERDICT 解析器
- `internal/agent/verdict_test.go` (新增) - 解析器测试
- `internal/agent/verification_gate_test.go` (新增) - gate 测试
- `internal/agent/verification_rewind_test.go` (新增) - 挽回测试
- `internal/agent/agent.go` (修改) - 验证 gate + 分级挽回
- `internal/agent/events.go` (修改) - LoopComplete.Verified + VerificationEvent
- `internal/agents/agent_tool.go` (修改) - RunVerification 方法
- `internal/agents/loader.go` (修改) - 默认启用 verification
- `internal/agents/loader_test.go` (修改) - 适配默认启用
- `internal/config/config.go` (修改) - EnableVerification 字段
- `internal/filehistory/filehistory.go` (修改) - Rewind 回滚增强
- `internal/tui/tui.go` (修改) - 接线 VerificationGate
- `internal/remote/server.go` (修改) - 接线 VerificationGate
- `cmd/mewcode/main.go` (修改) - config 接线
- `.mewcode/config.yaml.example` (修改) - enable_verification 字段
- `docs/git-history.md` (新增) - 本文件
- `docs/verification-gate-changelog.md` (新增) - 本次改动变更日志
