package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoopRegistry(t *testing.T) {
	r := NewNoopRegistry()
	if r.Enabled() {
		t.Fatal("noop registry should not be enabled")
	}
	if r.Handler() != nil {
		t.Fatal("noop registry should return nil handler")
	}

	m := NewMetrics(r)
	m.RecordAgentRun("success")
	m.RecordLLMCall("anthropic", "claude-3", "ok")
	m.ObserveTurnLatency(0)
	m.RecordToolCall("Bash", "ok")
	m.SetConversationTokens(1000)
	m.RecordPanic("agent_run")
}

func TestNoopIsNilSafe(t *testing.T) {
	var m *Metrics
	m.RecordAgentRun("success")
	m.RecordLLMCall("anthropic", "claude-3", "ok")
	m.ObserveTurnLatency(0)
	m.SetConversationTokens(1000)
	m.IncActiveSessions()
	m.DecActiveSessions()
}

func TestPrometheusRegistry(t *testing.T) {
	r := NewPrometheusRegistry()
	if !r.Enabled() {
		t.Fatal("prometheus registry should be enabled")
	}

	m := NewMetrics(r)
	m.RecordAgentRun("success")
	m.RecordAgentRun("error")
	m.RecordLLMCall("anthropic", "claude-3", "ok")
	m.RecordLLMCall("anthropic", "claude-3", "ratelimit")
	m.RecordTokenUsage("anthropic", "claude-3", 100, 50, 200, 10)
	m.ObserveLLMLatency("anthropic", "claude-3", 0)
	m.ObserveTurnLatency(0)
	m.RecordToolCall("Bash", "ok")
	m.ObserveToolLatency("Bash", 0)
	m.RecordToolRejected("EditFile", "permission")
	m.RecordVerification("pass")
	m.RecordCompaction("success")
	m.SetConversationTokens(5000)
	m.RecordPanic("agent_run")
	m.RecordError("network")
	m.IncActiveSessions()

	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("scrape failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(body)

	checks := []string{
		"mewcode_agent_run_total",
		"mewcode_llm_calls_total",
		"mewcode_llm_tokens_total",
		"mewcode_tool_calls_total",
		"mewcode_verification_results_total",
		"mewcode_compaction_runs_total",
		"mewcode_panics_total",
		"mewcode_errors_total",
		"mewcode_active_sessions",
		"mewcode_conversation_tokens",
	}
	for _, name := range checks {
		if !strings.Contains(text, name) {
			t.Errorf("metric %q not found in scrape output", name)
		}
	}

	if !strings.Contains(text, `outcome="success"`) {
		t.Error("agent_run_total success label not found")
	}
	if !strings.Contains(text, `status="ratelimit"`) {
		t.Error("llm_calls_total ratelimit label not found")
	}
}

func TestPrometheusNoDuplicatePanic(t *testing.T) {
	r := NewPrometheusRegistry()
	m1 := NewMetrics(r)
	m2 := NewMetrics(r)

	m1.RecordAgentRun("success")
	m2.RecordAgentRun("error")
}
