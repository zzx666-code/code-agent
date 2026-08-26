// Package eval provides a deterministic, offline harness for end-to-end agent
// task evaluation. A caller supplies an Executor backed by the real Agent (or
// a scripted test double); the harness owns timing, token accounting, and
// failure classification.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Category identifies the primary workflow exercised by a task.
type Category string

const (
	CategoryRetrieval Category = "code_retrieval"
	CategoryBugFix    Category = "bug_fix"
	CategoryFileEdit  Category = "file_modification"
	CategoryCommand   Category = "command_execution"
	CategoryRefactor  Category = "multi_step_refactor"
)

// Task is a single benchmark case. Prompt and SuccessCriteria are deliberately
// plain text so the same fixture can be sent to different model providers.
type Task struct {
	ID              string   `json:"id"`
	Category        Category `json:"category"`
	Prompt          string   `json:"prompt"`
	SuccessCriteria string   `json:"success_criteria"`
	Tags            []string `json:"tags,omitempty"`
}

// Execution is returned by an Executor. Duration is optional: Run measures
// wall-clock time around every invocation and uses this value only when set by
// an executor (for example, to report provider-side latency).
type Execution struct {
	Success       bool
	InputTokens   int
	OutputTokens  int
	FailureReason string
	Duration      time.Duration
	Output        string
}

// Executor runs one task against an Agent or a deterministic test double.
type Executor func(context.Context, Task) Execution

// TaskResult contains the measured outcome of one task.
type TaskResult struct {
	TaskID        string   `json:"task_id"`
	Category      Category `json:"category"`
	Success       bool     `json:"success"`
	DurationMs    int64    `json:"duration_ms"`
	InputTokens   int      `json:"input_tokens"`
	OutputTokens  int      `json:"output_tokens"`
	FailureReason string   `json:"failure_reason,omitempty"`
}

// CategorySummary aggregates outcomes for one task category.
type CategorySummary struct {
	Category      Category `json:"category"`
	Tasks         int      `json:"tasks"`
	Successful    int      `json:"successful"`
	SuccessRate   float64  `json:"success_rate"`
	AvgDurationMs float64  `json:"avg_duration_ms"`
}

// Report is the complete machine-readable evaluation result.
type Report struct {
	StartedAt         time.Time         `json:"started_at"`
	FinishedAt        time.Time         `json:"finished_at"`
	Tasks             int               `json:"tasks"`
	Successful        int               `json:"successful"`
	SuccessRate       float64           `json:"success_rate"`
	AvgDurationMs     float64           `json:"avg_duration_ms"`
	TotalInputTokens  int               `json:"total_input_tokens"`
	TotalOutputTokens int               `json:"total_output_tokens"`
	AvgInputTokens    float64           `json:"avg_input_tokens"`
	AvgOutputTokens   float64           `json:"avg_output_tokens"`
	FailureReasons    map[string]int    `json:"failure_reasons,omitempty"`
	ByCategory        []CategorySummary `json:"by_category,omitempty"`
	Results           []TaskResult      `json:"results"`
}

// Run executes tasks in fixture order. A nil executor is rejected rather than
// silently reporting an invalid benchmark.
func Run(ctx context.Context, tasks []Task, executor Executor) (Report, error) {
	if executor == nil {
		return Report{}, fmt.Errorf("eval: nil executor")
	}
	if len(tasks) == 0 {
		return Report{}, fmt.Errorf("eval: no tasks")
	}
	started := time.Now()
	report := Report{StartedAt: started, FailureReasons: map[string]int{}, Results: make([]TaskResult, 0, len(tasks))}
	category := map[Category]*CategorySummary{}
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		begin := time.Now()
		exec := executor(ctx, task)
		elapsed := time.Since(begin)
		if exec.Duration > 0 {
			elapsed = exec.Duration
		}
		result := TaskResult{TaskID: task.ID, Category: task.Category, Success: exec.Success,
			DurationMs: elapsed.Milliseconds(), InputTokens: max0(exec.InputTokens), OutputTokens: max0(exec.OutputTokens), FailureReason: exec.FailureReason}
		report.Results = append(report.Results, result)
		report.Tasks++
		report.TotalInputTokens += result.InputTokens
		report.TotalOutputTokens += result.OutputTokens
		if result.Success {
			report.Successful++
		} else {
			reason := result.FailureReason
			if reason == "" {
				reason = "unknown"
				result.FailureReason = reason
				report.Results[len(report.Results)-1].FailureReason = reason
			}
			report.FailureReasons[reason]++
		}
		s := category[task.Category]
		if s == nil {
			s = &CategorySummary{Category: task.Category}
			category[task.Category] = s
		}
		s.Tasks++
		s.AvgDurationMs += float64(result.DurationMs)
		if result.Success {
			s.Successful++
		}
	}
	report.FinishedAt = time.Now()
	if report.Tasks > 0 {
		report.SuccessRate = float64(report.Successful) / float64(report.Tasks)
		report.AvgDurationMs = averageDuration(report.Results)
		report.AvgInputTokens = float64(report.TotalInputTokens) / float64(report.Tasks)
		report.AvgOutputTokens = float64(report.TotalOutputTokens) / float64(report.Tasks)
	}
	for _, s := range category {
		if s.Tasks > 0 {
			s.SuccessRate = float64(s.Successful) / float64(s.Tasks)
			s.AvgDurationMs /= float64(s.Tasks)
		}
		report.ByCategory = append(report.ByCategory, *s)
	}
	sort.Slice(report.ByCategory, func(i, j int) bool { return report.ByCategory[i].Category < report.ByCategory[j].Category })
	return report, nil
}

func averageDuration(results []TaskResult) float64 {
	var total int64
	for _, r := range results {
		total += r.DurationMs
	}
	if len(results) == 0 {
		return 0
	}
	return float64(total) / float64(len(results))
}
func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// JSON returns an indented report suitable for CI artifacts.
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// DefaultExecutor is an offline smoke executor. It models a successful tool
// loop without network or filesystem dependencies and is useful for validating
// the fixture set and reporting pipeline. Production evaluation should inject
// an executor that invokes agent.Agent.Run.
func DefaultExecutor(ctx context.Context, task Task) Execution {
	if err := ctx.Err(); err != nil {
		return Execution{FailureReason: "cancelled"}
	}
	// Stable estimates make baseline comparisons reproducible across machines.
	return Execution{Success: true, InputTokens: len(task.Prompt)/4 + 8, OutputTokens: len(task.SuccessCriteria)/4 + 12}
}
