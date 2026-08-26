package eval

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDefaultTasksCoverRequiredScenarios(t *testing.T) {
	tasks := DefaultTasks()
	if len(tasks) < 20 {
		t.Fatalf("got %d tasks, want at least 20", len(tasks))
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.ID == "" || task.Prompt == "" || task.SuccessCriteria == "" {
			t.Fatalf("incomplete task: %+v", task)
		}
		if seen[task.ID] {
			t.Fatalf("duplicate task id %q", task.ID)
		}
		seen[task.ID] = true
	}
	for _, category := range []Category{CategoryRetrieval, CategoryBugFix, CategoryFileEdit, CategoryCommand, CategoryRefactor} {
		found := false
		for _, task := range tasks {
			if task.Category == category {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("category %q is not represented", category)
		}
	}
}

func TestRunAggregatesMetricsAndFailures(t *testing.T) {
	tasks := DefaultTasks()[:3]
	report, err := Run(context.Background(), tasks, func(ctx context.Context, task Task) Execution {
		if task.ID == tasks[1].ID {
			return Execution{FailureReason: "tool_error", InputTokens: 4, OutputTokens: 2}
		}
		return Execution{Success: true, InputTokens: 10, OutputTokens: 5}
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Tasks != 3 || report.Successful != 2 || report.SuccessRate != 2.0/3.0 {
		t.Fatalf("unexpected summary: %+v", report)
	}
	if report.FailureReasons["tool_error"] != 1 {
		t.Fatalf("failure reasons: %+v", report.FailureReasons)
	}
	if report.TotalInputTokens != 24 || report.TotalOutputTokens != 12 {
		t.Fatalf("token totals: %+v", report)
	}
	if len(report.ByCategory) != 1 || report.ByCategory[0].Tasks != 3 {
		t.Fatalf("category summary: %+v", report.ByCategory)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatal(err)
	}
}

func TestRunCancellationAndValidation(t *testing.T) {
	if _, err := Run(context.Background(), nil, DefaultExecutor); err == nil {
		t.Fatal("expected empty task error")
	}
	if _, err := Run(context.Background(), DefaultTasks()[:1], nil); err == nil {
		t.Fatal("expected nil executor error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, DefaultTasks(), DefaultExecutor); err == nil {
		t.Fatal("expected cancellation")
	}
}
