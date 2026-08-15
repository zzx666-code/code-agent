package trace

import "encoding/json"

const SchemaVersion = 1

const (
	TypeRunStart     = "run_start"
	TypeLLMCall      = "llm_call"
	TypeToolUse      = "tool_use"
	TypeToolResult   = "tool_result"
	TypeTurnEnd      = "turn_end"
	TypeRetry        = "retry"
	TypeCompact      = "compact"
	TypeVerification = "verification"
	TypeError        = "error"
	TypeRunEnd       = "run_end"
)

type Envelope struct {
	V           int             `json:"v"`
	Seq         int             `json:"seq"`
	TsMs        int64           `json:"ts_ms"`
	RunID       string          `json:"run_id"`
	ParentRunID string          `json:"parent_run_id,omitempty"`
	Turn        int             `json:"turn"`
	Type        string          `json:"type"`
	Data        json.RawMessage `json:"data"`
}

type Usage struct {
	InputTokens         int `json:"input"`
	OutputTokens        int `json:"output"`
	CacheReadTokens     int `json:"cache_read"`
	CacheCreationTokens int `json:"cache_creation"`
}

type RunStartData struct {
	Origin       string `json:"origin"`
	Model        string `json:"model,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	WorkDir      string `json:"work_dir,omitempty"`
	PromptDigest string `json:"prompt,omitempty"`
}

type LLMCallData struct {
	Turn       int    `json:"turn"`
	TtftMs     int64  `json:"ttft_ms"`
	ElapsedMs  int64  `json:"elapsed_ms"`
	StopReason string `json:"stop_reason,omitempty"`
	Usage      Usage  `json:"usage"`
}

type ToolUseData struct {
	Turn      int    `json:"turn"`
	ToolUseID string `json:"tool_use_id"`
	Tool      string `json:"tool"`
	Args      string `json:"args,omitempty"`
}

type ToolResultData struct {
	Turn          int    `json:"turn"`
	ToolUseID     string `json:"tool_use_id"`
	Tool          string `json:"tool"`
	IsError       bool   `json:"is_error"`
	ElapsedMs     int64  `json:"elapsed_ms"`
	OutputPreview string `json:"output_preview,omitempty"`
}

type TurnEndData struct {
	Turn         int    `json:"turn"`
	AssistantLen int    `json:"assistant_chars"`
	TextPreview  string `json:"text_preview,omitempty"`
}

type RetryData struct {
	Reason string `json:"reason"`
	WaitMs int64  `json:"wait_ms"`
}

type CompactData struct {
	Message string `json:"message"`
}

type VerificationData struct {
	Verdict  string `json:"verdict"`
	Retry    int    `json:"retry"`
	MaxRetry int    `json:"max_retry"`
}

type ErrorData struct {
	Message string `json:"message"`
}

type RunEndData struct {
	Outcome    string `json:"outcome"`
	TotalTurns int    `json:"total_turns"`
	WallMs     int64  `json:"wall_ms"`
	UsageTotal Usage  `json:"usage_total"`
	Verified   bool   `json:"verified,omitempty"`
	Dropped    int    `json:"dropped,omitempty"`
}
