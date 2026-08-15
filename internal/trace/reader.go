package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func tracesDir(workDir string) string {
	return filepath.Join(workDir, ".mewcode", "traces")
}

func TracesDirPath(workDir string) string {
	return tracesDir(workDir)
}

func bucketFile(workDir string, start time.Time) string {
	return filepath.Join(tracesDir(workDir), start.Format("2006-01-02"),
		fmtBucketName(start.Hour()))
}

func fmtBucketName(hour int) string {
	return pad2(hour) + "-" + pad2(hour+1) + ".jsonl"
}

func pad2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func LoadRecords(workDir, runID string) ([]Envelope, error) {
	start, err := ParseRunIDTime(runID)
	if err != nil {
		return nil, err
	}
	paths := []string{bucketFile(workDir, start)}
	next := start.Add(time.Hour)
	paths = append(paths, bucketFile(workDir, next))
	next = next.Add(time.Hour)
	paths = append(paths, bucketFile(workDir, next))

	var out []Envelope
	seen := map[int]bool{}
	for _, p := range paths {
		recs, err := readRecords(p)
		if err != nil {
			continue
		}
		for _, rec := range recs {
			if rec.RunID == runID && !seen[rec.Seq] {
				seen[rec.Seq] = true
				out = append(out, rec)
			}
		}
	}
	if len(out) == 0 {
		return nil, os.ErrNotExist
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

func readRecords(path string) ([]Envelope, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Envelope
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env Envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		out = append(out, env)
	}
	return out, scanner.Err()
}

type RunSummary struct {
	RunID       string
	ParentRunID string
	Start       time.Time
	Origin      string
	Model       string
	Outcome     string
	TotalTurns  int
	WallMs      int64
	Usage       Usage
	Verified    bool
	LLMCalls    int
	ToolCalls   int
	Errors      int
	Retries     int
}

func ListRuns(workDir, date string, hour int) ([]RunSummary, error) {
	base := tracesDir(workDir)
	dates := []string{}
	if date != "" {
		dates = append(dates, date)
	} else {
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				return []RunSummary{}, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && isDateDir(e.Name()) {
				dates = append(dates, e.Name())
			}
		}
		sort.Strings(dates)
		if len(dates) > 7 {
			dates = dates[len(dates)-7:]
		}
	}

	byRun := map[string]*RunSummary{}
	for _, d := range dates {
		dayDir := filepath.Join(base, d)
		files, err := os.ReadDir(dayDir)
		if err != nil {
			continue
		}
		for _, fe := range files {
			if fe.IsDir() || !strings.HasSuffix(fe.Name(), ".jsonl") {
				continue
			}
			if hour >= 0 && fe.Name() != fmtBucketName(hour) {
				continue
			}
			recs, err := readRecords(filepath.Join(dayDir, fe.Name()))
			if err != nil {
				continue
			}
			absorbRecords(byRun, recs)
		}
	}

	out := make([]RunSummary, 0, len(byRun))
	for _, s := range byRun {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func isDateDir(name string) bool {
	if len(name) != 10 {
		return false
	}
	if _, err := time.Parse("2006-01-02", name); err != nil {
		return false
	}
	return true
}

func absorbRecords(byRun map[string]*RunSummary, recs []Envelope) {
	for _, rec := range recs {
		s, ok := byRun[rec.RunID]
		if !ok {
			s = &RunSummary{RunID: rec.RunID, ParentRunID: rec.ParentRunID, Outcome: "incomplete"}
			if t, err := ParseRunIDTime(rec.RunID); err == nil {
				s.Start = t
			}
			byRun[rec.RunID] = s
		}
		switch rec.Type {
		case TypeRunStart:
			var d RunStartData
			if json.Unmarshal(rec.Data, &d) == nil {
				s.Origin = d.Origin
				s.Model = d.Model
			}
		case TypeLLMCall:
			s.LLMCalls++
			var d LLMCallData
			if json.Unmarshal(rec.Data, &d) == nil {
				s.Usage.InputTokens += d.Usage.InputTokens
				s.Usage.OutputTokens += d.Usage.OutputTokens
				s.Usage.CacheReadTokens += d.Usage.CacheReadTokens
				s.Usage.CacheCreationTokens += d.Usage.CacheCreationTokens
			}
		case TypeToolUse:
			s.ToolCalls++
		case TypeError:
			s.Errors++
		case TypeRetry:
			s.Retries++
		case TypeRunEnd:
			var d RunEndData
			if json.Unmarshal(rec.Data, &d) == nil {
				s.Outcome = d.Outcome
				s.TotalTurns = d.TotalTurns
				s.WallMs = d.WallMs
				s.Usage = d.UsageTotal
				s.Verified = d.Verified
			}
		}
	}
}
