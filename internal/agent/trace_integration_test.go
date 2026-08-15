package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"mewcode/internal/agent"
	"mewcode/internal/conversation"
	"mewcode/internal/llm"
	"mewcode/internal/tools"
	"mewcode/internal/trace"
)

type scriptedClient struct {
	responses [][]llm.StreamEvent
	callIdx   int
}

func (m *scriptedClient) SetSystemPrompt(string) {}
func (m *scriptedClient) Stream(ctx context.Context, conv *conversation.Manager, toolSchemas []map[string]any) (<-chan llm.StreamEvent, <-chan error) {
	ch := make(chan llm.StreamEvent, 64)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		defer close(errCh)
		if m.callIdx >= len(m.responses) {
			ch <- llm.TextDelta{Text: "[mock done]"}
			ch <- llm.StreamEnd{StopReason: "end_turn"}
			return
		}
		for _, ev := range m.responses[m.callIdx] {
			ch <- ev
		}
		m.callIdx++
	}()
	return ch, errCh
}

type echoTool struct{}

func (e *echoTool) Name() string                    { return "Echo" }
func (e *echoTool) Description() string             { return "echoes input" }
func (e *echoTool) Category() tools.ToolCategory    { return tools.CategoryRead }
func (e *echoTool) Schema() map[string]any {
	return map[string]any{
		"name":        "Echo",
		"description": "echoes input",
		"input_schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"msg": map[string]any{"type": "string"}},
		},
	}
}

func (e *echoTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	msg, _ := args["msg"].(string)
	return tools.ToolResult{Output: msg}
}

func TestTraceIntegration(t *testing.T) {
	dir := t.TempDir()
	client := &scriptedClient{
		responses: [][]llm.StreamEvent{
			{
				llm.ToolCallComplete{ToolID: "t1", ToolName: "Echo", Arguments: map[string]any{"msg": "hello"}},
				llm.StreamEnd{StopReason: "tool_use", Usage: llm.UsageInfo{
					InputTokens: 100, OutputTokens: 20, CacheReadTokens: 5, CacheCreationTokens: 7,
				}},
			},
			{
				llm.TextDelta{Text: "done"},
				llm.StreamEnd{StopReason: "end_turn", Usage: llm.UsageInfo{
					InputTokens: 150, OutputTokens: 10, CacheReadTokens: 100, CacheCreationTokens: 3,
				}},
			},
		},
	}

	reg := tools.NewRegistry()
	reg.Register(&echoTool{})

	ag := agent.New(client, reg, "anthropic")
	ag.TraceObserver = trace.NewRecorder(dir, trace.RunStartData{Origin: "test", Model: "m1"}, "")
	ag.MaxIterations = 5

	conv := conversation.NewManager()
	conv.AddUserMessage("test")

	ch := ag.Run(context.Background(), conv)
	var lastText string
	for ev := range ch {
		if st, ok := ev.(agent.StreamText); ok {
			lastText += st.Text
		}
	}
	if lastText != "done" {
		t.Fatalf("expected 'done', got %q", lastText)
	}

	records, err := trace.LoadRecords(dir, ag.TraceObserver.(*trace.Recorder).RunID())
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}

	llmCount := 0
	toolUseCount := 0
	toolResultCount := 0
	var totalInput, totalOutput int
	for _, rec := range records {
		switch rec.Type {
		case trace.TypeLLMCall:
			llmCount++
			var d trace.LLMCallData
			_ = json.Unmarshal(rec.Data, &d)
			totalInput += d.Usage.InputTokens
			totalOutput += d.Usage.OutputTokens
		case trace.TypeToolUse:
			toolUseCount++
		case trace.TypeToolResult:
			toolResultCount++
		case trace.TypeRunEnd:
			var d trace.RunEndData
			_ = json.Unmarshal(rec.Data, &d)
			if d.Outcome != "success" {
				t.Fatalf("expected success outcome, got %s", d.Outcome)
			}
			if d.TotalTurns != 2 {
				t.Fatalf("expected 2 turns, got %d", d.TotalTurns)
			}
		}
	}
	if llmCount != 2 {
		t.Fatalf("expected 2 llm_call records, got %d", llmCount)
	}
	if toolUseCount != 1 || toolResultCount != 1 {
		t.Fatalf("expected 1 tool_use + 1 tool_result, got %d/%d", toolUseCount, toolResultCount)
	}
	if totalInput != 250 || totalOutput != 30 {
		t.Fatalf("expected total in=250 out=30, got in=%d out=%d", totalInput, totalOutput)
	}
}