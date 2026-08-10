package agent

import (
	"context"
	"testing"
	"time"

	"mewcode/internal/conversation"
	"mewcode/internal/llm"
	"mewcode/internal/tools"
)

func editFileRound(id, path string) []llm.StreamEvent {
	return []llm.StreamEvent{
		llm.ToolCallStart{ToolName: "EditFile", ToolID: id},
		llm.ToolCallComplete{ToolID: id, ToolName: "EditFile", Arguments: map[string]any{"file_path": path}},
	}
}

func textRound(text string) []llm.StreamEvent {
	return []llm.StreamEvent{
		llm.TextDelta{Text: text},
		llm.StreamEnd{StopReason: "end_turn"},
	}
}

func regWithEditTool() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "EditFile", result: "edited"})
	return reg
}

func TestVerificationGate_PassCompletesVerified(t *testing.T) {
	r1 := append(editFileRound("e1", "a.go"), editFileRound("e2", "b.go")...)
	r1 = append(r1, editFileRound("e3", "c.go")...)
	r1 = append(r1, llm.StreamEnd{StopReason: "tool_use"})
	client := &mockClient{responses: [][]llm.StreamEvent{r1, textRound("done")}}
	ag := New(client, regWithEditTool(), "anthropic")
	ag.VerificationGate = func(ctx context.Context, conv *conversation.Manager) (Verdict, string, error) {
		return VerdictPass, "all good", nil
	}
	conv := conversation.NewManager()
	events := collectEvents(ag.Run(context.Background(), conv))

	var lc LoopComplete
	for _, ev := range events {
		if l, ok := ev.(LoopComplete); ok {
			lc = l
		}
	}
	if lc.TotalTurns == 0 {
		t.Fatal("no LoopComplete event")
	}
	if !lc.Verified {
		t.Error("LoopComplete.Verified should be true on PASS")
	}
}

func TestVerificationGate_FailThenPassRetries(t *testing.T) {
	r1 := append(editFileRound("e1", "a.go"), editFileRound("e2", "b.go")...)
	r1 = append(r1, editFileRound("e3", "c.go")...)
	r1 = append(r1, llm.StreamEnd{StopReason: "tool_use"})
	client := &mockClient{responses: [][]llm.StreamEvent{r1, textRound("done"), textRound("fixed")}}
	ag := New(client, regWithEditTool(), "anthropic")

	calls := 0
	ag.VerificationGate = func(ctx context.Context, conv *conversation.Manager) (Verdict, string, error) {
		calls++
		if calls == 1 {
			return VerdictFail, "tests failed", nil
		}
		return VerdictPass, "all good", nil
	}
	conv := conversation.NewManager()
	events := collectEvents(ag.Run(context.Background(), conv))

	var failEv, passEv *VerificationEvent
	var lc LoopComplete
	for _, ev := range events {
		if v, ok := ev.(VerificationEvent); ok {
			if v.Verdict == "FAIL" {
				failEv = &v
			}
			if v.Verdict == "PASS" {
				passEv = &v
			}
		}
		if l, ok := ev.(LoopComplete); ok {
			lc = l
		}
	}
	if failEv == nil {
		t.Error("expected a FAIL VerificationEvent")
	}
	if passEv == nil {
		t.Error("expected a PASS VerificationEvent after retry")
	}
	if failEv != nil && failEv.Retry != 0 {
		t.Errorf("FAIL retry = %d, want 0", failEv.Retry)
	}
	if passEv != nil && passEv.Retry != 1 {
		t.Errorf("PASS retry = %d, want 1 (one retry consumed)", passEv.Retry)
	}
	if !lc.Verified {
		t.Error("LoopComplete should be verified after PASS on retry")
	}
	if calls != 2 {
		t.Errorf("gate called %d times, want 2", calls)
	}
}

func TestVerificationGate_FailExhaustedCompletesUnverified(t *testing.T) {
	r1 := append(editFileRound("e1", "a.go"), editFileRound("e2", "b.go")...)
	r1 = append(r1, editFileRound("e3", "c.go")...)
	r1 = append(r1, llm.StreamEnd{StopReason: "tool_use"})
	client := &mockClient{responses: [][]llm.StreamEvent{r1, textRound("done"), textRound("fixed"), textRound("again")}}
	ag := New(client, regWithEditTool(), "anthropic")

	calls := 0
	ag.VerificationGate = func(ctx context.Context, conv *conversation.Manager) (Verdict, string, error) {
		calls++
		return VerdictFail, "still broken", nil
	}
	conv := conversation.NewManager()
	events := collectEvents(ag.Run(context.Background(), conv))

	var lc LoopComplete
	var failCount int
	for _, ev := range events {
		if v, ok := ev.(VerificationEvent); ok && v.Verdict == "FAIL" {
			failCount++
		}
		if l, ok := ev.(LoopComplete); ok {
			lc = l
		}
	}
	if failCount != maxVerifyRetries+1 {
		t.Errorf("FAIL events = %d, want %d (1 original + %d retries)", failCount, maxVerifyRetries+1, maxVerifyRetries)
	}
	if lc.Verified {
		t.Error("LoopComplete should be unverified after exhausting retries")
	}
	if calls != maxVerifyRetries+1 {
		t.Errorf("gate called %d times, want %d", calls, maxVerifyRetries+1)
	}
}

func TestVerificationGate_SkippedForTrivialChanges(t *testing.T) {
	r1 := append(editFileRound("e1", "a.go"), editFileRound("e2", "b.go")...)
	r1 = append(r1, llm.StreamEnd{StopReason: "tool_use"})
	client := &mockClient{responses: [][]llm.StreamEvent{r1, textRound("done")}}
	ag := New(client, regWithEditTool(), "anthropic")

	called := false
	ag.VerificationGate = func(ctx context.Context, conv *conversation.Manager) (Verdict, string, error) {
		called = true
		return VerdictPass, "", nil
	}
	conv := conversation.NewManager()
	events := collectEvents(ag.Run(context.Background(), conv))

	var lc LoopComplete
	for _, ev := range events {
		if l, ok := ev.(LoopComplete); ok {
			lc = l
		}
	}
	if called {
		t.Error("gate should NOT run for <3 file edits")
	}
	if !lc.Verified {
		t.Error("trivial changes should be Verified=true (gate skipped)")
	}
}

func TestShouldVerify(t *testing.T) {
	ag := &Agent{}
	conv := conversation.NewManager()
	if ag.shouldVerify(conv) {
		t.Error("empty conv should not verify")
	}
	conv.AddUserMessage("task")
	for i := 0; i < 3; i++ {
		conv.AddToolUseMessage("", "id"+string(rune(i)), "EditFile", map[string]any{"file_path": "f.go"})
	}
	if !ag.shouldVerify(conv) {
		t.Error("3 EditFile calls should verify")
	}
}

func TestVerificationGate_PartialCompletesVerified(t *testing.T) {
	r1 := append(editFileRound("e1", "a.go"), editFileRound("e2", "b.go")...)
	r1 = append(r1, editFileRound("e3", "c.go")...)
	r1 = append(r1, llm.StreamEnd{StopReason: "tool_use"})
	client := &mockClient{responses: [][]llm.StreamEvent{r1, textRound("done")}}
	ag := New(client, regWithEditTool(), "anthropic")
	ag.VerificationGate = func(ctx context.Context, conv *conversation.Manager) (Verdict, string, error) {
		return VerdictPartial, "env limited", nil
	}
	conv := conversation.NewManager()
	events := collectEvents(ag.Run(context.Background(), conv))

	var lc LoopComplete
	for _, ev := range events {
		if l, ok := ev.(LoopComplete); ok {
			lc = l
		}
	}
	if !lc.Verified {
		t.Error("PARTIAL should complete as verified (not FAIL)")
	}
}

func TestVerificationGate_CompletesWithinTimeout(t *testing.T) {
	r1 := append(editFileRound("e1", "a.go"), editFileRound("e2", "b.go")...)
	r1 = append(r1, editFileRound("e3", "c.go")...)
	r1 = append(r1, llm.StreamEnd{StopReason: "tool_use"})
	client := &mockClient{responses: [][]llm.StreamEvent{r1, textRound("done")}}
	ag := New(client, regWithEditTool(), "anthropic")
	ag.VerificationGate = func(ctx context.Context, conv *conversation.Manager) (Verdict, string, error) {
		return VerdictPass, "", nil
	}
	conv := conversation.NewManager()
	done := make(chan struct{})
	go func() {
		collectEvents(ag.Run(context.Background(), conv))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not complete within 5s")
	}
}
