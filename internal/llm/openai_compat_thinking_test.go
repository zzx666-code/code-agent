package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/conversation"
)

func TestOpenAICompatThinkingFieldSent(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dec := json.NewDecoder(r.Body)
		var body map[string]any
		_ = dec.Decode(&body)
		captured = body
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"index\":0}],\"object\":\"chat.completion.chunk\"}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}],\"object\":\"chat.completion.chunk\"}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
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
	events, _ := client.Stream(context.Background(), conv, nil)
	for range events {
	}

	thinking, ok := captured["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking field missing from request body: %v", captured)
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking.type = %v, want enabled", thinking["type"])
	}
	if !strings.Contains(srv.URL, "127.0.0.1") {
		t.Fatal("sanity")
	}
}
