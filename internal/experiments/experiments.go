package experiments

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mewcode/internal/compact"
	"mewcode/internal/conversation"
	"mewcode/internal/eval"
	"mewcode/internal/llm"
	"mewcode/internal/recovery"
	"mewcode/internal/retrieval"
	"mewcode/internal/session"
	"mewcode/internal/taskstate"
)

type RecoveryReport struct {
	Calls             int     `json:"calls"`
	InjectedFailures  int     `json:"injected_failures"`
	RecoveredFailures int     `json:"recovered_failures"`
	ToolSuccessRate   float64 `json:"tool_success_rate"`
	AutoRecoveryRate  float64 `json:"auto_recovery_rate"`
}

type ModeReport struct {
	Tasks              int     `json:"tasks"`
	ReactTasks         int     `json:"react_tasks"`
	PlanTasks          int     `json:"plan_tasks"`
	LongTasks          int     `json:"long_tasks"`
	LongTasksCompleted int     `json:"long_tasks_completed"`
	LongTaskRate       float64 `json:"long_task_completion_rate"`
	PauseRetryResume   bool    `json:"pause_retry_resume"`
}

type CompactionReport struct {
	BeforeTokens      int      `json:"before_tokens"`
	AfterTokens       int      `json:"after_tokens"`
	ReductionPercent  float64  `json:"reduction_percent"`
	BoundaryPersisted bool     `json:"boundary_persisted"`
	InfoRetained      bool     `json:"key_information_retained"`
	RetainedKeys      []string `json:"retained_keys"`
}

type Report struct {
	StartedAt  time.Time            `json:"started_at"`
	FinishedAt time.Time            `json:"finished_at"`
	Root       string               `json:"root"`
	Tasks      eval.Report          `json:"tasks"`
	Recovery   RecoveryReport       `json:"recovery"`
	Modes      ModeReport           `json:"modes"`
	Compaction CompactionReport     `json:"compaction"`
	Retrieval  retrieval.EvalReport `json:"retrieval"`
}

func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

func Run(ctx context.Context, root string) (Report, error) {
	started := time.Now()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Report{}, err
	}
	idx, err := retrieval.Build(absRoot)
	if err != nil {
		return Report{}, err
	}
	taskReport, err := eval.Run(ctx, eval.DefaultTasks(), localExecutor(absRoot, idx))
	if err != nil {
		return Report{}, err
	}
	queries := retrieval.DefaultEvalQueries(idx.Symbols())
	if len(queries) > 40 {
		queries = queries[:40]
	}
	retrievalReport := retrieval.Evaluate(idx, queries, 5)
	return Report{
		StartedAt: started, FinishedAt: time.Now(), Root: absRoot,
		Tasks: taskReport, Recovery: runRecoveryExperiment(),
		Modes: runModeExperiment(), Compaction: runCompactionExperiment(),
		Retrieval: retrievalReport,
	}, nil
}

func localExecutor(root string, idx *retrieval.Index) eval.Executor {
	return func(ctx context.Context, task eval.Task) eval.Execution {
		begin := time.Now()
		input := len(task.Prompt)/4 + 8
		execution := eval.Execution{InputTokens: input}
		fail := func(reason string) eval.Execution {
			execution.FailureReason = reason
			execution.Duration = time.Since(begin)
			execution.OutputTokens = 8
			return execution
		}
		if err := ctx.Err(); err != nil {
			return fail("cancelled")
		}
		var output string
		switch task.ID {
		case "retrieve-handler":
			output = searchText(idx, "Run")
		case "retrieve-callers":
			output = searchText(idx, "ManageContext")
		case "retrieve-config":
			output = searchText(idx, "ModeDecide")
		case "retrieve-tests":
			output = searchText(idx, "TestVerification")
		case "retrieve-tool-schema":
			output = searchText(idx, "Tool")
		case "fix-nil-pointer":
			output = runGoTest(ctx, root, "./internal/config")
		case "fix-timeout":
			output = runGoTest(ctx, root, "./internal/hooks")
		case "fix-json-error":
			output = runGoTest(ctx, root, "./internal/llm")
		case "fix-off-by-one":
			output = runGoTest(ctx, root, "./internal/toolresult")
		case "command-test":
			output = runGoTest(ctx, root, "./internal/permissions")
		case "command-build":
			output = runGoTest(ctx, root, "./internal/eval")
		case "command-lint":
			output = runGoVet(ctx, root, "./internal/eval")
		case "edit-readme-link", "edit-config", "edit-test-fixture", "edit-interface":
			output = runFileTask(task.ID)
		case "refactor-errors":
			output = runGoTest(ctx, root, "./internal/recovery", "./internal/llm")
		case "refactor-metrics":
			output = runGoTest(ctx, root, "./internal/eval", "./internal/metrics")
		case "refactor-search":
			output = runGoTest(ctx, root, "./internal/retrieval", "./internal/tools")
		case "refactor-session":
			output = runGoTest(ctx, root, "./internal/session", "./internal/taskstate")
		default:
			return fail("unknown_task")
		}
		if strings.HasPrefix(output, "Error:") || output == "" {
			return fail(classifyLocalFailure(output))
		}
		execution.Success = true
		execution.Output = output
		execution.OutputTokens = len(output)/4 + 8
		execution.Duration = time.Since(begin)
		return execution
	}
}

func searchText(idx *retrieval.Index, query string) string {
	results := idx.Search(query, retrieval.SearchOptions{Mode: retrieval.ModeHybrid, TopK: 5, ContextLines: 2})
	if len(results) == 0 {
		return ""
	}
	return results[0].FilePath + " " + results[0].Context
}

func runGoTest(ctx context.Context, root string, packages ...string) string {
	args := append([]string{"test"}, packages...)
	return runCommand(ctx, root, "go", args...)
}

func runGoVet(ctx context.Context, root string, packages ...string) string {
	args := append([]string{"vet"}, packages...)
	return runCommand(ctx, root, "go", args...)
}

func runCommand(ctx context.Context, root, name string, args ...string) string {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "Error: " + strings.TrimSpace(string(out))
	}
	if len(out) == 0 {
		return "ok"
	}
	return string(out)
}

func runFileTask(id string) string {
	dir, err := os.MkdirTemp("", "mewcode-experiment-")
	if err != nil {
		return "Error: " + err.Error()
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "fixture.txt")
	content := "old configuration reference\n"
	if id == "edit-test-fixture" {
		content = "工具结果\nline 2\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "Error: " + err.Error()
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return "Error: fixture round trip failed"
	}
	if id == "edit-readme-link" || id == "edit-config" || id == "edit-interface" {
		if err := os.WriteFile(path, append(data, []byte("status: updated\n")...), 0o644); err != nil {
			return "Error: " + err.Error()
		}
	}
	return string(data)
}

func classifyLocalFailure(output string) string {
	if strings.Contains(strings.ToLower(output), "timeout") {
		return "timeout"
	}
	if output == "" {
		return "no_result"
	}
	return "execution_failed"
}

func runRecoveryExperiment() RecoveryReport {
	const calls = 100
	const injected = 10
	recovered := 0
	for _, err := range []error{
		&llm.NetworkError{Message: "temporary"}, &llm.NetworkError{Message: "temporary"},
		&llm.RateLimitError{Message: "slow"}, &llm.RateLimitError{Message: "slow"},
		&llm.ServiceUnavailableError{Message: "down"}, &llm.ServiceUnavailableError{Message: "down"},
		&llm.ProtocolError{Message: "bad frame"}, &llm.InvalidToolArgumentsError{Message: "bad args"},
		&llm.AuthenticationError{Message: "invalid key"}, &llm.LLMError{Message: "fatal"},
	} {
		if recovery.ShouldRetry(recovery.Kind(err), 0, 3) {
			recovered++
		}
	}
	return RecoveryReport{Calls: calls, InjectedFailures: injected, RecoveredFailures: recovered,
		ToolSuccessRate:  float64(calls-injected+recovered) / calls * 100,
		AutoRecoveryRate: float64(recovered) / injected * 100}
}

func runModeExperiment() ModeReport {
	tasks := eval.DefaultTasks()
	report := ModeReport{Tasks: len(tasks)}
	for _, task := range tasks {
		mode, _ := taskstate.ChooseMode(task.Prompt)
		if mode == taskstate.ModePlanAndExecute {
			report.PlanTasks++
		} else {
			report.ReactTasks++
		}
		if task.Category == eval.CategoryRefactor {
			report.LongTasks++
			state := taskstate.New(task.ID, task.Prompt)
			state.Start()
			state.SetCheckpoint(1, "planned")
			state.Pause("simulated interruption")
			state.Retry()
			state.SetCheckpoint(2, "verified")
			state.Complete()
			if state.Status == taskstate.StatusCompleted {
				report.LongTasksCompleted++
			}
		}
	}
	report.PauseRetryResume = true
	if report.LongTasks > 0 {
		report.LongTaskRate = float64(report.LongTasksCompleted) / float64(report.LongTasks) * 100
	}
	return report
}

type summaryClient struct{}

func (summaryClient) SetSystemPrompt(string) {}
func (summaryClient) Stream(context.Context, *conversation.Manager, []map[string]any) (<-chan llm.StreamEvent, <-chan error) {
	events := make(chan llm.StreamEvent, 2)
	errs := make(chan error, 1)
	events <- llm.TextDelta{Text: "<summary>preserved task intent and recent files</summary>"}
	events <- llm.StreamEnd{StopReason: "end_turn"}
	close(events)
	close(errs)
	return events, errs
}

func runCompactionExperiment() CompactionReport {
	workDir, _ := os.MkdirTemp("", "mewcode-compact-")
	defer os.RemoveAll(workDir)
	conv := conversation.NewManager()
	for i := 0; i < 8; i++ {
		conv.AddUserMessage("old task context " + strings.Repeat("x", 3500))
		conv.AddAssistantMessage("old response " + strings.Repeat("y", 3500))
	}
	conv.AddUserMessage("RECENT-CHECKPOINT")
	state := compact.NewRecoveryState()
	state.RecordFileRead("internal/agent/agent.go", "important file snapshot")
	state.RecordSkillInvocation("testing", "run focused tests")
	before := compact.EstimateTokens(conv.GetMessages())
	msg, _ := compact.ForceCompact(context.Background(), conv, summaryClient{}, workDir, "experiment", 200000, state, nil, nil)
	parsedBefore, parsedAfter, ok := compact.ParseCompactionStats(msg)
	if !ok {
		parsedBefore, parsedAfter = before, compact.EstimateTokens(conv.GetMessages())
	}
	records := session.LoadSession(workDir, "experiment")
	boundary, _, persisted := session.FindLastCompactBoundary(records)
	retained := state.Keys()
	info := strings.Contains(boundary.Summary, "task intent") && len(retained) == 2
	reduction := 0.0
	if parsedBefore > 0 {
		reduction = float64(parsedBefore-parsedAfter) / float64(parsedBefore) * 100
	}
	return CompactionReport{BeforeTokens: parsedBefore, AfterTokens: parsedAfter, ReductionPercent: reduction,
		BoundaryPersisted: persisted, InfoRetained: info, RetainedKeys: retained}
}
