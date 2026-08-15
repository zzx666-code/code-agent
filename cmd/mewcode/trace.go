package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"mewcode/internal/trace"
)

func runTraceCommand(args []string) error {
	if len(args) == 0 {
		printTraceUsage()
		return nil
	}
	wd, _ := os.Getwd()
	switch args[0] {
	case "list":
		return runTraceList(wd, args[1:])
	case "show":
		return runTraceShow(wd, args[1:])
	case "top":
		return runTraceTop(wd, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown trace subcommand: %s\n", args[0])
		printTraceUsage()
		os.Exit(1)
	}
	return nil
}

func printTraceUsage() {
	fmt.Fprint(os.Stderr, `usage:
  mewcode trace list [--date 2006-01-02] [--hour 0-23]
  mewcode trace show <run_id> [--errors] [--json]
  mewcode trace top  <run_id> --by tokens|latency
`)
}

func runTraceList(wd string, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	date := fs.String("date", "", "filter by date dir (2006-01-02)")
	hour := fs.Int("hour", -1, "filter by hour bucket (0-23)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	runs, err := trace.ListRuns(wd, *date, *hour)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Println("no traces found under", trace.TracesDirPath(wd))
		return nil
	}
	fmt.Printf("%-26s %-8s %-24s %-16s %5s %12s %10s %s\n",
		"RUN_ID", "ORIGIN", "MODEL", "START", "TURNS", "TOKENS", "WALL", "OUTCOME")
	for _, r := range runs {
		start := "-"
		if !r.Start.IsZero() {
			start = r.Start.Format("01-02 15:04:05")
		}
		fmt.Printf("%-26s %-8s %-24s %-16s %5d %12s %10s %s\n",
			r.RunID, orDash(r.Origin), orDash(r.Model), start,
			r.TotalTurns, fmtTokens(r.Usage.InputTokens+r.Usage.OutputTokens),
			fmtDur(r.WallMs), r.Outcome)
	}
	return nil
}

func runTraceShow(wd string, args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	errorsOnly := fs.Bool("errors", false, "only show error/retry steps")
	jsonOut := fs.Bool("json", false, "dump raw records as JSONL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("trace show requires a run_id")
	}
	runID := fs.Arg(0)
	records, err := trace.LoadRecords(wd, runID)
	if err != nil {
		return fmt.Errorf("trace for run %s not found under %s", runID, trace.TracesDirPath(wd))
	}
	if *jsonOut {
		for _, rec := range records {
			b, _ := json.Marshal(rec)
			fmt.Println(string(b))
		}
		return nil
	}
	renderTrace(records, *errorsOnly)
	return nil
}

type traceStep struct {
	kind    string
	turn    int
	tsMs    int64
	tool    string
	args    string
	output  string
	isError bool
	elapsed int64
	ttft    int64
	stop    string
	usage   trace.Usage
}

func renderTrace(records []trace.Envelope, errorsOnly bool) {
	var steps []traceStep
	byTool := map[string]int{}
	runHeader := trace.RunStartData{}
	var runEnd *trace.RunEndData

	for i := range records {
		rec := records[i]
		switch rec.Type {
		case trace.TypeRunStart:
			json.Unmarshal(rec.Data, &runHeader)
		case trace.TypeRunEnd:
			var d trace.RunEndData
			if json.Unmarshal(rec.Data, &d) == nil {
				runEnd = &d
			}
		case trace.TypeLLMCall:
			var d trace.LLMCallData
			if json.Unmarshal(rec.Data, &d) == nil {
				steps = append(steps, traceStep{kind: "llm", turn: d.Turn, tsMs: rec.TsMs,
					elapsed: d.ElapsedMs, ttft: d.TtftMs, stop: d.StopReason, usage: d.Usage})
			}
		case trace.TypeToolUse:
			var d trace.ToolUseData
			if json.Unmarshal(rec.Data, &d) == nil {
				steps = append(steps, traceStep{kind: "tool", turn: d.Turn, tsMs: rec.TsMs,
					tool: d.Tool, args: d.Args})
				byTool[d.ToolUseID] = len(steps) - 1
			}
		case trace.TypeToolResult:
			var d trace.ToolResultData
			if json.Unmarshal(rec.Data, &d) == nil {
				if idx, ok := byTool[d.ToolUseID]; ok {
					steps[idx].output = d.OutputPreview
					steps[idx].isError = d.IsError
					steps[idx].elapsed = d.ElapsedMs
				} else {
					steps = append(steps, traceStep{kind: "tool", turn: d.Turn, tsMs: rec.TsMs,
						tool: d.Tool, output: d.OutputPreview,
						isError: d.IsError, elapsed: d.ElapsedMs})
				}
			}
		case trace.TypeRetry, trace.TypeCompact, trace.TypeVerification, trace.TypeError:
			msg := ""
			switch rec.Type {
			case trace.TypeRetry:
				var d trace.RetryData
				json.Unmarshal(rec.Data, &d)
				msg = "retry: " + d.Reason
			case trace.TypeCompact:
				var d trace.CompactData
				json.Unmarshal(rec.Data, &d)
				msg = "compact: " + d.Message
			case trace.TypeVerification:
				var d trace.VerificationData
				json.Unmarshal(rec.Data, &d)
				msg = fmt.Sprintf("verification: %s (retry %d/%d)", d.Verdict, d.Retry, d.MaxRetry)
			case trace.TypeError:
				var d trace.ErrorData
				json.Unmarshal(rec.Data, &d)
				msg = "error: " + d.Message
			}
			steps = append(steps, traceStep{kind: rec.Type, turn: rec.Turn, tsMs: rec.TsMs, args: msg})
		}
	}

	fmt.Printf("RUN %s  %s  %s", records[0].RunID, orDash(runHeader.Origin), orDash(runHeader.Model))
	if runEnd != nil {
		verified := ""
		if runEnd.Verified {
			verified = " verified"
		}
		fmt.Printf("  outcome=%s%s  wall=%s  turns=%d  tokens in=%s out=%s (cache_read=%s)",
			runEnd.Outcome, verified, fmtDur(runEnd.WallMs), runEnd.TotalTurns,
			fmtTokens(runEnd.UsageTotal.InputTokens), fmtTokens(runEnd.UsageTotal.OutputTokens),
			fmtTokens(runEnd.UsageTotal.CacheReadTokens))
	}
	fmt.Println()

	currentTurn := -1
	for _, s := range steps {
		if s.turn != currentTurn {
			currentTurn = s.turn
			if errorsOnly && !turnHasError(steps, s.turn) {
				continue
			}
			fmt.Printf("\nTURN %d\n", s.turn)
		}
		if errorsOnly && !stepIsError(s) {
			continue
		}
		renderStep(s)
	}
}

func turnHasError(steps []traceStep, turn int) bool {
	for _, s := range steps {
		if s.turn == turn && stepIsError(s) {
			return true
		}
	}
	return false
}

func stepIsError(s traceStep) bool {
	return s.isError || s.kind == trace.TypeError || s.kind == trace.TypeRetry
}

func renderStep(s traceStep) {
	switch s.kind {
	case "llm":
		ttft := ""
		if s.ttft > 0 {
			ttft = fmt.Sprintf(" (ttft %s)", fmtDur(s.ttft))
		}
		fmt.Printf("  llm    %s%s  stop=%s  in %s (cache %s) out %s tok\n",
			fmtDur(s.elapsed), ttft, orDash(s.stop),
			fmtTokens(s.usage.InputTokens), fmtTokens(s.usage.CacheReadTokens),
			fmtTokens(s.usage.OutputTokens))
	case "tool":
		status := "ok"
		if s.isError {
			status = "ERROR"
		}
		arg := firstLine(s.args)
		if arg != "" {
			arg = " " + truncateRunes(arg, 48)
		}
		fmt.Printf("  %-7s%-49s %-5s %7s\n", s.tool, arg, status, fmtDur(s.elapsed))
		if s.isError && s.output != "" {
			for _, line := range strings.Split(truncateRunes(s.output, 400), "\n") {
				fmt.Printf("         | %s\n", line)
			}
		}
	default:
		fmt.Printf("  [%s] %s\n", s.kind, firstLine(s.args))
	}
}

func runTraceTop(wd string, args []string) error {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	by := fs.String("by", "tokens", "rank by: tokens | latency")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("trace top requires a run_id")
	}
	runID := fs.Arg(0)
	records, err := trace.LoadRecords(wd, runID)
	if err != nil {
		return fmt.Errorf("trace for run %s not found under %s", runID, trace.TracesDirPath(wd))
	}

	type entry struct {
		label   string
		turn    int
		tokens  int
		latency int64
		isError bool
	}
	var entries []entry
	for _, rec := range records {
		switch rec.Type {
		case trace.TypeLLMCall:
			var d trace.LLMCallData
			if json.Unmarshal(rec.Data, &d) == nil {
				total := d.Usage.InputTokens + d.Usage.OutputTokens + d.Usage.CacheCreationTokens
				entries = append(entries, entry{
					label:   fmt.Sprintf("llm  in %s (cache_read %s) out %s", fmtTokens(d.Usage.InputTokens), fmtTokens(d.Usage.CacheReadTokens), fmtTokens(d.Usage.OutputTokens)),
					turn:    d.Turn,
					tokens:  total,
					latency: d.ElapsedMs,
				})
			}
		case trace.TypeToolResult:
			var d trace.ToolResultData
			if json.Unmarshal(rec.Data, &d) == nil {
				entries = append(entries, entry{
					label:   fmt.Sprintf("tool %s", d.Tool),
					turn:    d.Turn,
					latency: d.ElapsedMs,
					isError: d.IsError,
				})
			}
		}
	}
	if len(entries) == 0 {
		fmt.Println("no steps recorded for this run")
		return nil
	}

	switch *by {
	case "tokens":
		for i := range entries {
			if entries[i].tokens == 0 {
				entries[i].tokens = -1
			}
		}
		sortEntries(entries, func(a, b entry) bool { return a.tokens > b.tokens })
		fmt.Printf("TOP STEPS BY TOKENS  run=%s\n", runID)
		for i, e := range entries {
			if e.tokens < 0 {
				break
			}
			fmt.Printf("  #%d  TURN %-4d %s\n", i+1, e.turn, e.label)
		}
	case "latency":
		sortEntries(entries, func(a, b entry) bool { return a.latency > b.latency })
		fmt.Printf("TOP STEPS BY LATENCY  run=%s\n", runID)
		for i, e := range entries {
			mark := ""
			if e.isError {
				mark = "  ERROR"
			}
			fmt.Printf("  #%d  TURN %-4d %s  %s%s\n", i+1, e.turn, e.label, fmtDur(e.latency), mark)
		}
	default:
		return fmt.Errorf("--by must be tokens or latency")
	}
	return nil
}

func sortEntries[E any](e []E, less func(a, b E) bool) {
	for i := 1; i < len(e); i++ {
		for j := i; j > 0 && less(e[j], e[j-1]); j-- {
			e[j], e[j-1] = e[j-1], e[j]
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func fmtDur(ms int64) string {
	if ms < 0 {
		return "-"
	}
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + "ms"
	}
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func fmtTokens(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
