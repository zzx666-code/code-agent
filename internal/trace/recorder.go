package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mewcode/internal/agent"
	"mewcode/internal/session"
)

const (
	argsLimit       = 4096
	outputLimit     = 2048
	textLimit       = 2048
	promptLimit     = 2048
	runIDTimeLayout = "20060102-150405"
)

var writeMu sync.Mutex

type Recorder struct {
	runID       string
	parentRunID string
	meta        RunStartData

	file     *os.File
	start    time.Time
	seq      int
	dropped  int
	closeOne sync.Once

	currentTurn int
	textChars   int
	textTail    strings.Builder
	lastError   string
	sawRunEnd   bool
	verified    bool
	totalTurns  int
	usageTotal  Usage
}

func NewRecorder(workDir string, meta RunStartData, parentRunID string) *Recorder {
	r := &Recorder{
		runID:       session.NewID(),
		parentRunID: parentRunID,
		meta:        meta,
		start:       time.Now(),
	}
	r.meta.PromptDigest = truncateString(r.meta.PromptDigest, promptLimit)
	if f, err := openBucketFile(workDir, r.start); err == nil {
		r.file = f
	} else {
		r.dropped++
	}
	r.write(TypeRunStart, 0, r.meta)
	return r
}

func (r *Recorder) RunID() string { return r.runID }

func (r *Recorder) ParentRunID() string { return r.parentRunID }

func openBucketFile(workDir string, start time.Time) (*os.File, error) {
	dayDir := filepath.Join(workDir, ".mewcode", "traces", start.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%02d-%02d.jsonl", start.Hour(), start.Hour()+1)
	return os.OpenFile(filepath.Join(dayDir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

func truncateString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…[truncated]"
}

func (r *Recorder) write(typ string, turn int, data any) {
	if r.file == nil {
		r.dropped++
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		r.dropped++
		return
	}
	env := Envelope{
		V:           SchemaVersion,
		Seq:         r.seq,
		TsMs:        time.Now().UnixMilli(),
		RunID:       r.runID,
		ParentRunID: r.parentRunID,
		Turn:        turn,
		Type:        typ,
		Data:        raw,
	}
	line, err := json.Marshal(env)
	if err != nil {
		r.dropped++
		return
	}
	line = append(line, '\n')
	writeMu.Lock()
	if _, err := r.file.Write(line); err != nil {
		r.dropped++
	}
	writeMu.Unlock()
	r.seq++
}

func (r *Recorder) ObserveAgentEvent(ev agent.AgentEvent) {
	switch e := ev.(type) {
	case agent.LLMStartEvent:
		r.currentTurn = e.Turn
	case agent.LLMEndEvent:
		r.currentTurn = e.Turn
		u := Usage{
			InputTokens:         e.Usage.InputTokens,
			OutputTokens:        e.Usage.OutputTokens,
			CacheReadTokens:     e.Usage.CacheReadTokens,
			CacheCreationTokens: e.Usage.CacheCreationTokens,
		}
		r.usageTotal.InputTokens += u.InputTokens
		r.usageTotal.OutputTokens += u.OutputTokens
		r.usageTotal.CacheReadTokens += u.CacheReadTokens
		r.usageTotal.CacheCreationTokens += u.CacheCreationTokens
		r.write(TypeLLMCall, e.Turn, LLMCallData{
			Turn:       e.Turn,
			TtftMs:     e.TTFT.Milliseconds(),
			ElapsedMs:  e.Elapsed.Milliseconds(),
			StopReason: e.StopReason,
			Usage:      u,
		})
	case agent.ToolUseEvent:
		args := ""
		if e.Args != nil {
			if b, err := json.Marshal(e.Args); err == nil {
				args = truncateString(string(b), argsLimit)
			}
		}
		r.write(TypeToolUse, r.currentTurn, ToolUseData{
			Turn:      r.currentTurn,
			ToolUseID: e.ToolID,
			Tool:      e.ToolName,
			Args:      args,
		})
	case agent.ToolResultEvent:
		r.write(TypeToolResult, r.currentTurn, ToolResultData{
			Turn:          r.currentTurn,
			ToolUseID:     e.ToolID,
			Tool:          e.ToolName,
			IsError:       e.IsError,
			ElapsedMs:     e.Elapsed.Milliseconds(),
			OutputPreview: truncateString(e.Output, outputLimit),
		})
	case agent.StreamText:
		r.textChars += len(e.Text)
		tail := e.Text
		if b := r.textTail.String(); len(b)+len(tail) > textLimit {
			keep := textLimit - len(tail)
			if keep < 0 {
				keep = 0
			}
			r.textTail.Reset()
			r.textTail.WriteString(b[len(b)-keep:])
		}
		r.textTail.WriteString(tail)
	case agent.TurnComplete:
		r.totalTurns = e.Turn
		r.write(TypeTurnEnd, e.Turn, TurnEndData{
			Turn:         e.Turn,
			AssistantLen: r.textChars,
			TextPreview:  truncateString(r.textTail.String(), textLimit),
		})
		r.textChars = 0
		r.textTail.Reset()
	case agent.RetryEvent:
		r.write(TypeRetry, r.currentTurn, RetryData{Reason: e.Reason, WaitMs: e.Wait.Milliseconds()})
	case agent.CompactEvent:
		r.write(TypeCompact, r.currentTurn, CompactData{Message: truncateString(e.Message, outputLimit)})
	case agent.VerificationEvent:
		r.write(TypeVerification, r.currentTurn, VerificationData{
			Verdict:  e.Verdict,
			Retry:    e.Retry,
			MaxRetry: e.MaxRetry,
		})
	case agent.ErrorEvent:
		r.lastError = e.Message
		r.write(TypeError, r.currentTurn, ErrorData{Message: truncateString(e.Message, outputLimit)})
	case agent.LoopComplete:
		r.sawRunEnd = true
		r.verified = e.Verified
		r.totalTurns = e.TotalTurns
		r.writeRunEnd("success")
	}
}

func (r *Recorder) writeRunEnd(outcome string) {
	r.write(TypeRunEnd, r.currentTurn, RunEndData{
		Outcome:    outcome,
		TotalTurns: r.totalTurns,
		WallMs:     time.Since(r.start).Milliseconds(),
		UsageTotal: r.usageTotal,
		Verified:   r.verified,
		Dropped:    r.dropped,
	})
}

func (r *Recorder) TraceClose() {
	r.closeOne.Do(func() {
		if !r.sawRunEnd {
			outcome := "incomplete"
			if r.lastError != "" {
				outcome = "error"
			}
			r.writeRunEnd(outcome)
		}
		if r.file != nil {
			writeMu.Lock()
			r.file.Close()
			writeMu.Unlock()
		}
	})
}

func ParseRunIDTime(runID string) (time.Time, error) {
	if len(runID) < len(runIDTimeLayout) {
		return time.Time{}, fmt.Errorf("invalid run id: %s", runID)
	}
	return time.ParseInLocation(runIDTimeLayout, runID[:len(runIDTimeLayout)], time.Local)
}
