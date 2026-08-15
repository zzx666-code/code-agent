package trace

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mewcode/internal/agent"
	"mewcode/internal/llm"
)

func readEnvelope(t *testing.T, path string) []Envelope {
	t.Helper()
	recs, err := readRecords(path)
	if err != nil {
		t.Fatalf("readRecords: %v", err)
	}
	return recs
}

func findType(recs []Envelope, typ string) *Envelope {
	for i := range recs {
		if recs[i].Type == typ {
			return &recs[i]
		}
	}
	return nil
}

func TestRecorderEventMapping(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(dir, RunStartData{Origin: "test", Model: "m1", SessionID: "s1", PromptDigest: "do stuff"}, "")

	r.ObserveAgentEvent(agent.LLMStartEvent{Turn: 1})
	r.ObserveAgentEvent(agent.StreamText{Text: "hello "})
	r.ObserveAgentEvent(agent.StreamText{Text: "world"})
	r.ObserveAgentEvent(agent.ToolUseEvent{ToolID: "t1", ToolName: "Bash", Args: map[string]any{"command": "go test"}})
	r.ObserveAgentEvent(agent.ToolResultEvent{ToolID: "t1", ToolName: "Bash", Output: "PASS", Elapsed: 1500 * time.Millisecond})
	r.ObserveAgentEvent(agent.LLMEndEvent{Turn: 1, StopReason: "tool_use", Usage: llm.UsageInfo{
		InputTokens: 100, OutputTokens: 20, CacheReadTokens: 5, CacheCreationTokens: 7,
	}, Elapsed: 3 * time.Second, TTFT: 800 * time.Millisecond})
	r.ObserveAgentEvent(agent.TurnComplete{Turn: 1})
	r.ObserveAgentEvent(agent.LoopComplete{TotalTurns: 1, Verified: true})
	r.TraceClose()

	start := r.start
	path := bucketFile(dir, start)
	recs := readEnvelope(t, path)
	if len(recs) != 6 {
		t.Fatalf("expected 6 records, got %d", len(recs))
	}

	if rs := findType(recs, TypeRunStart); rs == nil {
		t.Fatal("missing run_start")
	} else {
		var d RunStartData
		json.Unmarshal(rs.Data, &d)
		if d.Origin != "test" || d.Model != "m1" {
			t.Fatalf("run_start data mismatch: %+v", d)
		}
		if rs.RunID != r.RunID() {
			t.Fatalf("run_id mismatch: %s vs %s", rs.RunID, r.RunID())
		}
	}

	lc := findType(recs, TypeLLMCall)
	if lc == nil {
		t.Fatal("missing llm_call")
	}
	var ld LLMCallData
	json.Unmarshal(lc.Data, &ld)
	if ld.Turn != 1 || ld.StopReason != "tool_use" || ld.ElapsedMs != 3000 || ld.TtftMs != 800 {
		t.Fatalf("llm_call fields mismatch: %+v", ld)
	}
	if ld.Usage.InputTokens != 100 || ld.Usage.OutputTokens != 20 || ld.Usage.CacheReadTokens != 5 || ld.Usage.CacheCreationTokens != 7 {
		t.Fatalf("llm_call usage mismatch: %+v", ld.Usage)
	}
	if lc.Turn != 1 {
		t.Fatalf("llm_call turn mismatch: %d", lc.Turn)
	}

	tu := findType(recs, TypeToolUse)
	if tu == nil {
		t.Fatal("missing tool_use")
	}
	var ud ToolUseData
	json.Unmarshal(tu.Data, &ud)
	if ud.ToolUseID != "t1" || ud.Tool != "Bash" || !strings.Contains(ud.Args, "go test") {
		t.Fatalf("tool_use data mismatch: %+v", ud)
	}
	if tu.Turn != 1 {
		t.Fatalf("tool_use turn mismatch: %d", tu.Turn)
	}

	tr := findType(recs, TypeToolResult)
	if tr == nil {
		t.Fatal("missing tool_result")
	}
	var rd ToolResultData
	json.Unmarshal(tr.Data, &rd)
	if rd.IsError || rd.ElapsedMs != 1500 || rd.OutputPreview != "PASS" {
		t.Fatalf("tool_result data mismatch: %+v", rd)
	}

	te := findType(recs, TypeTurnEnd)
	if te == nil {
		t.Fatal("missing turn_end")
	}
	var td TurnEndData
	json.Unmarshal(te.Data, &td)
	if td.AssistantLen != 11 || !strings.Contains(td.TextPreview, "world") {
		t.Fatalf("turn_end data mismatch: %+v", td)
	}

	re := findType(recs, TypeRunEnd)
	if re == nil {
		t.Fatal("missing run_end")
	}
	var ed RunEndData
	json.Unmarshal(re.Data, &ed)
	if ed.Outcome != "success" || ed.TotalTurns != 1 || !ed.Verified {
		t.Fatalf("run_end data mismatch: %+v", ed)
	}
	if ed.UsageTotal.InputTokens != 100 || ed.UsageTotal.OutputTokens != 20 {
		t.Fatalf("run_end usage mismatch: %+v", ed.UsageTotal)
	}
}

func TestRecorderErrorOutcome(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(dir, RunStartData{Origin: "test"}, "")
	r.ObserveAgentEvent(agent.ErrorEvent{Message: "boom"})
	r.TraceClose()
	r.TraceClose()

	recs := readEnvelope(t, bucketFile(dir, r.start))
	re := findType(recs, TypeRunEnd)
	if re == nil {
		t.Fatal("missing run_end")
	}
	var ed RunEndData
	json.Unmarshal(re.Data, &ed)
	if ed.Outcome != "error" {
		t.Fatalf("expected error outcome, got %s", ed.Outcome)
	}
}

func TestRecorderTruncation(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(dir, RunStartData{Origin: "test"}, "")
	long := strings.Repeat("x", argsLimit+100)
	r.ObserveAgentEvent(agent.ToolUseEvent{ToolID: "t1", ToolName: "Read", Args: map[string]any{"path": long}})
	r.ObserveAgentEvent(agent.ToolResultEvent{ToolID: "t1", ToolName: "Read", Output: strings.Repeat("y", outputLimit+50)})
	r.TraceClose()

	recs := readEnvelope(t, bucketFile(dir, r.start))
	var ud ToolUseData
	json.Unmarshal(findType(recs, TypeToolUse).Data, &ud)
	if len(ud.Args) > argsLimit+50 {
		t.Fatalf("args not truncated: %d", len(ud.Args))
	}
	var rd ToolResultData
	json.Unmarshal(findType(recs, TypeToolResult).Data, &rd)
	if len(rd.OutputPreview) > outputLimit+50 {
		t.Fatalf("output not truncated: %d", len(rd.OutputPreview))
	}
}

func TestLoadRecordsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(dir, RunStartData{Origin: "test"}, "")
	r.ObserveAgentEvent(agent.LLMEndEvent{Turn: 1, StopReason: "end_turn", Usage: llm.UsageInfo{InputTokens: 10, OutputTokens: 5}, Elapsed: time.Second})
	r.ObserveAgentEvent(agent.LoopComplete{TotalTurns: 1})
	r.TraceClose()

	recs, err := LoadRecords(dir, r.RunID())
	if err != nil {
		t.Fatalf("LoadRecords: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}
	if _, err := LoadRecords(dir, "bogus"); err == nil {
		t.Fatal("expected error for bogus run id")
	}
}

func TestBucketNaming(t *testing.T) {
	if got := fmtBucketName(9); got != "09-10.jsonl" {
		t.Fatalf("bucket name: %s", got)
	}
	if got := fmtBucketName(23); got != "23-24.jsonl" {
		t.Fatalf("bucket name: %s", got)
	}
}

func TestListRunsAggregation(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(dir, RunStartData{Origin: "test", Model: "m1"}, "")
	r.ObserveAgentEvent(agent.LLMEndEvent{Turn: 1, Usage: llm.UsageInfo{InputTokens: 100, OutputTokens: 50}, Elapsed: time.Second})
	r.ObserveAgentEvent(agent.ToolUseEvent{ToolID: "t1", ToolName: "Bash"})
	r.ObserveAgentEvent(agent.ErrorEvent{Message: "x"})
	r.ObserveAgentEvent(agent.LoopComplete{TotalTurns: 1})
	r.TraceClose()

	runs, err := ListRuns(dir, r.start.Format("2006-01-02"), -1)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	s := runs[0]
	if s.RunID != r.RunID() || s.Origin != "test" || s.Model != "m1" || s.Outcome != "success" {
		t.Fatalf("summary mismatch: %+v", s)
	}
	if s.LLMCalls != 1 || s.ToolCalls != 1 || s.Errors != 1 {
		t.Fatalf("summary counters mismatch: %+v", s)
	}
	if s.Usage.InputTokens != 100 || s.Usage.OutputTokens != 50 {
		t.Fatalf("summary usage mismatch: %+v", s.Usage)
	}

	byHour, err := ListRuns(dir, "", r.start.Hour())
	if err != nil {
		t.Fatalf("ListRuns by hour: %v", err)
	}
	if len(byHour) != 1 {
		t.Fatalf("expected 1 run in hour bucket, got %d", len(byHour))
	}
}

func TestConcurrentRecordersShareBucket(t *testing.T) {
	dir := t.TempDir()
	r1 := NewRecorder(dir, RunStartData{Origin: "a"}, "")
	r2 := NewRecorder(dir, RunStartData{Origin: "b"}, "")
	if r1.RunID() == r2.RunID() {
		t.Fatal("run ids should differ")
	}
	for i := 0; i < 50; i++ {
		r1.ObserveAgentEvent(agent.LLMEndEvent{Turn: 1, Usage: llm.UsageInfo{InputTokens: i}, Elapsed: time.Millisecond})
		r2.ObserveAgentEvent(agent.LLMEndEvent{Turn: 1, Usage: llm.UsageInfo{InputTokens: i}, Elapsed: time.Millisecond})
	}
	r1.TraceClose()
	r2.TraceClose()

	path := bucketFile(dir, r1.start)
	recs := readEnvelope(t, path)
	counts := map[string]int{}
	for _, rec := range recs {
		if rec.Type == TypeLLMCall {
			counts[rec.RunID]++
		}
	}
	if counts[r1.RunID()] != 50 || counts[r2.RunID()] != 50 {
		t.Fatalf("expected 50 llm_call per run, got %v", counts)
	}
}

func TestParentRunIDPersisted(t *testing.T) {
	dir := t.TempDir()
	parent := NewRecorder(dir, RunStartData{Origin: "tui"}, "")
	child := NewRecorder(dir, RunStartData{Origin: "subagent"}, parent.RunID())
	child.ObserveAgentEvent(agent.LoopComplete{TotalTurns: 1})
	child.TraceClose()
	parent.TraceClose()

	recs := readEnvelope(t, bucketFile(dir, child.start))
	for _, rec := range recs {
		if rec.RunID != child.RunID() || rec.Type != TypeRunStart {
			continue
		}
		if rec.ParentRunID != parent.RunID() {
			t.Fatalf("parent_run_id mismatch: %s", rec.ParentRunID)
		}
		return
	}
	t.Fatal("child run_start not found")
}

func TestParseRunIDTime(t *testing.T) {
	if _, err := ParseRunIDTime("short"); err == nil {
		t.Fatal("expected error for short id")
	}
	ts, err := ParseRunIDTime("20260815-101500-ab")
	if err != nil {
		t.Fatalf("ParseRunIDTime: %v", err)
	}
	if ts.Hour() != 10 || ts.Minute() != 15 {
		t.Fatalf("parsed time mismatch: %v", ts)
	}
}
