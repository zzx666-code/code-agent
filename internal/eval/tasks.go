package eval

// DefaultTasks is the offline benchmark suite. Cases are intentionally small,
// but each represents a workflow found in day-to-day coding-agent usage.
func DefaultTasks() []Task {
	return []Task{
		{ID: "retrieve-handler", Category: CategoryRetrieval, Prompt: "Locate the HTTP handler that creates an order and report its package and function.", SuccessCriteria: "internal/orders.CreateOrderHandler", Tags: []string{"symbol", "http"}},
		{ID: "retrieve-callers", Category: CategoryRetrieval, Prompt: "Find all callers of compact.ManageContext and identify the recovery path.", SuccessCriteria: "agent.Run and handleStreamError", Tags: []string{"callers"}},
		{ID: "retrieve-config", Category: CategoryRetrieval, Prompt: "Locate where permission_mode is parsed and list the default value.", SuccessCriteria: "internal/config config loader", Tags: []string{"config"}},
		{ID: "retrieve-tests", Category: CategoryRetrieval, Prompt: "Find tests covering verification rewind and summarize the assertion for failed verification.", SuccessCriteria: "verification_rewind_test.go", Tags: []string{"tests"}},
		{ID: "retrieve-tool-schema", Category: CategoryRetrieval, Prompt: "Locate the Tool interface and explain how a tool schema is exposed.", SuccessCriteria: "internal/tools.Tool Schema", Tags: []string{"interface"}},
		{ID: "fix-nil-pointer", Category: CategoryBugFix, Prompt: "Fix a nil pointer panic when an optional provider configuration is omitted, then add a regression test.", SuccessCriteria: "guard nil config and test", Tags: []string{"regression"}},
		{ID: "fix-timeout", Category: CategoryBugFix, Prompt: "Make a blocked tool execution honor context cancellation and return a useful error.", SuccessCriteria: "context cancellation is propagated", Tags: []string{"error", "context"}},
		{ID: "fix-json-error", Category: CategoryBugFix, Prompt: "Fix malformed tool arguments so invalid JSON produces a structured tool error instead of terminating the loop.", SuccessCriteria: "structured argument error and continued loop", Tags: []string{"tool", "json"}},
		{ID: "fix-off-by-one", Category: CategoryBugFix, Prompt: "Correct the off-by-one truncation in token budgeting and add boundary tests for zero and exact limits.", SuccessCriteria: "boundary tests pass", Tags: []string{"tokens"}},
		{ID: "edit-readme-link", Category: CategoryFileEdit, Prompt: "Update the stale configuration example reference in README.md and preserve surrounding formatting.", SuccessCriteria: "only intended README line changed", Tags: []string{"docs"}},
		{ID: "edit-config", Category: CategoryFileEdit, Prompt: "Add an explicit timeout field to the example config with a conservative default.", SuccessCriteria: "config.yaml.example contains timeout", Tags: []string{"config"}},
		{ID: "edit-test-fixture", Category: CategoryFileEdit, Prompt: "Create a regression fixture for a tool result containing Unicode and newlines.", SuccessCriteria: "fixture is loadable and round-trips", Tags: []string{"fixture"}},
		{ID: "edit-interface", Category: CategoryFileEdit, Prompt: "Add a status field to the evaluation result type and update its JSON tags.", SuccessCriteria: "status serializes as status", Tags: []string{"api"}},
		{ID: "command-test", Category: CategoryCommand, Prompt: "Run the focused package tests for internal/permissions and report the command and result.", SuccessCriteria: "go test ./internal/permissions passes", Tags: []string{"go"}},
		{ID: "command-build", Category: CategoryCommand, Prompt: "Run go build ./... and diagnose any compile failure without changing unrelated files.", SuccessCriteria: "build succeeds or failure is explained", Tags: []string{"go", "build"}},
		{ID: "command-lint", Category: CategoryCommand, Prompt: "Run go vet ./... and fix only actionable diagnostics in the touched package.", SuccessCriteria: "vet completes cleanly", Tags: []string{"go", "vet"}},
		{ID: "refactor-errors", Category: CategoryRefactor, Prompt: "Refactor repeated LLM error classification into a helper, update callers, and preserve retry behavior with tests.", SuccessCriteria: "all error classes retain their retry policy", Tags: []string{"multi-step", "errors"}},
		{ID: "refactor-metrics", Category: CategoryRefactor, Prompt: "Introduce a typed task metrics report, wire token and latency aggregation, and expose JSON output.", SuccessCriteria: "report has totals, averages, and failure reasons", Tags: []string{"multi-step", "metrics"}},
		{ID: "refactor-search", Category: CategoryRefactor, Prompt: "Replace duplicate code-search formatting with one renderer, update symbol and keyword paths, and run tests.", SuccessCriteria: "both search paths produce equivalent context", Tags: []string{"multi-step", "search"}},
		{ID: "refactor-session", Category: CategoryRefactor, Prompt: "Add resumable task state to the session boundary, persist it, reload it, and verify old sessions remain readable.", SuccessCriteria: "new and legacy session formats load", Tags: []string{"multi-step", "session"}},
	}
}
