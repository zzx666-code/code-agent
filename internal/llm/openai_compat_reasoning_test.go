package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mewcode/internal/config"
	"mewcode/internal/conversation"
)

func TestOpenAICompatReasoningContentParsedAsThinkingDelta(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"\",\"reasoning_content\":\"step 1: analyze\",\"role\":\"assistant\"},\"index\":0}],\"created\":1787049473,\"id\":\"x\",\"model\":\"glm-5-2\",\"object\":\"chat.completion.chunk\"}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"answer\",\"role\":\"assistant\"},\"index\":0}],\"created\":1787049473,\"id\":\"x\",\"model\":\"glm-5-2\",\"object\":\"chat.completion.chunk\"}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}],\"created\":1787049473,\"id\":\"x\",\"model\":\"glm-5-2\",\"object\":\"chat.completion.chunk\"}\n\n" +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sse))
	}))
	defer srv.Close()

	cfg := &config.ProviderConfig{
		Name:     "test",
		Protocol: "openai-compat",
		BaseURL:  srv.URL,
		APIKey:   "k",
		Model:    "glm-5.2",
		Thinking: true,
	}
	client, err := newOpenAICompatClient(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	conv := conversation.NewManager()
	conv.AddUserMessage("hi")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, errs := client.Stream(ctx, conv, nil)

	var thinkingText, finalText string
	var sawThinkingDelta bool
	for ev := range events {
		switch e := ev.(type) {
		case ThinkingDelta:
			sawThinkingDelta = true
			thinkingText += e.Text
		case TextDelta:
			finalText += e.Text
		}
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
	default:
	}

	if !sawThinkingDelta {
		t.Fatal("no ThinkingDelta event emitted - SDK did not surface reasoning_content via ExtraFields")
	}
	if thinkingText != "step 1: analyze" {
		t.Fatalf("thinking text = %q, want %q", thinkingText, "step 1: analyze")
	}
	if finalText != "answer" {
		t.Fatalf("final text = %q, want %q", finalText, "answer")
	}
}
