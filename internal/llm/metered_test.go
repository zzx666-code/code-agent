package llm

import (
	"context"
	"testing"
	"time"

	"mewcode/internal/conversation"
	"mewcode/internal/metrics"
)

type mockClient struct {
	events []StreamEvent
	err    error
}

func (m *mockClient) SetSystemPrompt(prompt string) {}

func (m *mockClient) Stream(ctx context.Context, conv *conversation.Manager, tools []map[string]any) (<-chan StreamEvent, <-chan error) {
	evCh := make(chan StreamEvent, len(m.events))
	errCh := make(chan error, 1)
	for _, ev := range m.events {
		evCh <- ev
	}
	close(evCh)
	if m.err != nil {
		errCh <- m.err
	}
	close(errCh)
	return evCh, errCh
}

func TestMeteredClientSuccess(t *testing.T) {
	reg := metrics.NewPrometheusRegistry()
	m := metrics.NewMetrics(reg)

	mock := &mockClient{
		events: []StreamEvent{
			TextDelta{Text: "hello"},
			StreamEnd{
				StopReason: "end_turn",
				Usage: UsageInfo{
					InputTokens:         100,
					OutputTokens:        50,
					CacheReadTokens:     20,
					CacheCreationTokens: 5,
				},
			},
		},
	}

	mc := NewMeteredClient(mock, m, "anthropic", "claude-3")
	conv := conversation.NewManager()
	events, errs := mc.Stream(context.Background(), conv, nil)

	count := 0
	for ev := range events {
		count++
		_ = ev
	}
	if count != 2 {
		t.Fatalf("expected 2 events, got %d", count)
	}

	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	default:
	}

	time.Sleep(10 * time.Millisecond)
}

func TestMeteredClientError(t *testing.T) {
	reg := metrics.NewPrometheusRegistry()
	m := metrics.NewMetrics(reg)

	mock := &mockClient{
		events: []StreamEvent{},
		err:    &RateLimitError{Message: "rate limited", RetryAfter: "5"},
	}

	mc := NewMeteredClient(mock, m, "openai", "gpt-4")
	conv := conversation.NewManager()
	events, errs := mc.Stream(context.Background(), conv, nil)

	for range events {
	}

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected error")
		}
	default:
		t.Fatal("expected error in channel")
	}

	time.Sleep(10 * time.Millisecond)
}

func TestClassifyErrorStatus(t *testing.T) {
	tests := []struct {
		err    error
		status string
	}{
		{&AuthenticationError{Message: "bad key"}, "auth"},
		{&RateLimitError{Message: "slow down"}, "ratelimit"},
		{&NetworkError{Message: "timeout"}, "network"},
		{&ContextTooLongError{Message: "too long"}, "context"},
		{&LLMError{Message: "500"}, "server"},
		{nil, "unknown"},
	}
	for _, tt := range tests {
		if tt.err == nil {
			continue
		}
		got := ClassifyErrorStatus(tt.err)
		if got != tt.status {
			t.Errorf("ClassifyErrorStatus(%T) = %q, want %q", tt.err, got, tt.status)
		}
	}
}

func TestMeteredClientNilMetrics(t *testing.T) {
	mock := &mockClient{
		events: []StreamEvent{StreamEnd{StopReason: "end_turn"}},
	}
	mc := NewMeteredClient(mock, nil, "anthropic", "claude-3")
	conv := conversation.NewManager()
	events, _ := mc.Stream(context.Background(), conv, nil)

	count := 0
	for range events {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 event, got %d", count)
	}
}

func TestMeteredClientForwardsSetSystemPrompt(t *testing.T) {
	mock := &mockClient{}
	mc := NewMeteredClient(mock, metrics.Noop, "anthropic", "claude-3")
	mc.SetSystemPrompt("test")
}
