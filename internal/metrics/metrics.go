package metrics

import (
	"net/http"
	"time"
)

type Registry interface {
	Counter(name, help string, labels ...string) Counter
	Histogram(name, help string, buckets []float64, labels ...string) Histogram
	Gauge(name, help string, labels ...string) Gauge
	Handler() http.Handler
	Enabled() bool
}

type Counter interface {
	Inc(labelValues ...string)
	Add(v float64, labelValues ...string)
}

type Histogram interface {
	Observe(v float64, labelValues ...string)
}

type Gauge interface {
	Set(v float64, labelValues ...string)
	Inc(labelValues ...string)
	Dec(labelValues ...string)
}

var LatencyBuckets = []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300}

var TurnBuckets = []float64{1, 5, 10, 20, 50, 100, 200, 500}

var RetryBuckets = []float64{1, 2, 3, 5, 8, 13}

type Metrics struct {
	agentRunTotal         Counter
	verificationResults   Counter
	verificationRetries   Histogram
	turnsPerRun           Histogram
	turnLatency           Histogram
	llmLatency            Histogram
	llmCallsTotal         Counter
	llmTokensTotal        Counter
	llmRetryTotal         Counter
	toolCallsTotal        Counter
	toolLatency           Histogram
	toolRejectedTotal     Counter
	compactionRunsTotal   Counter
	compactionLatency     Histogram
	conversationTokens    Gauge
	verificationRetriesG  Gauge
	panicsTotal           Counter
	errorsTotal           Counter
	sessionSaveErrors     Counter
	activeSessions        Gauge
	activeTools           Gauge
	toolresultSpilled     Counter
	subagentSpawnTotal    Counter
	subagentDuration      Histogram
	subagentFailureTotal  Counter
	activeTeammates       Gauge

	requestsTotal          Counter
	activeTasks            Gauge
	taskDuration           Histogram
	stepsTotal             Counter
	maxStepsReachedTotal   Counter
	taskTimeoutTotal       Counter
	llmErrorsTotal         Counter
	llmCostTotal           Counter
	toolErrorsTotal        Counter
	filesModifiedTotal     Counter
	codeEditsTotal         Counter
	commandExecutionsTotal Counter
	buildTotal             Counter
	buildSuccessTotal      Counter
	testsTotal             Counter
	testsPassedTotal       Counter
	testsFailedTotal       Counter
}

func NewMetrics(r Registry) *Metrics {
	return &Metrics{
		agentRunTotal:        r.Counter("mewcode_agent_run_total", "Total agent runs by outcome", "outcome"),
		verificationResults:  r.Counter("mewcode_verification_results_total", "Verification gate verdicts", "verdict"),
		verificationRetries:  r.Histogram("mewcode_verification_retries", "Verification retry count distribution", RetryBuckets),
		turnsPerRun:          r.Histogram("mewcode_turns_per_run", "Turns per agent run", TurnBuckets),
		turnLatency:          r.Histogram("mewcode_turn_latency_seconds", "Agent turn latency in seconds", LatencyBuckets),
		llmLatency:           r.Histogram("mewcode_llm_latency_seconds", "LLM stream latency in seconds", LatencyBuckets, "provider", "model"),
		llmCallsTotal:        r.Counter("mewcode_llm_calls_total", "Total LLM calls by provider/model/status", "provider", "model", "status"),
		llmTokensTotal:       r.Counter("mewcode_llm_tokens_total", "Total LLM tokens consumed", "provider", "model", "type"),
		llmRetryTotal:        r.Counter("mewcode_llm_retry_total", "LLM call retries by error type", "type"),
		toolCallsTotal:       r.Counter("mewcode_tool_calls_total", "Total tool calls by name/status", "name", "status"),
		toolLatency:          r.Histogram("mewcode_tool_latency_seconds", "Tool execution latency in seconds", LatencyBuckets, "name"),
		toolRejectedTotal:    r.Counter("mewcode_tool_rejected_total", "Tool calls rejected by permissions or hooks", "name", "reason"),
		compactionRunsTotal:  r.Counter("mewcode_compaction_runs_total", "Context compaction runs by result", "result"),
		compactionLatency:    r.Histogram("mewcode_compaction_latency_seconds", "Compaction latency in seconds", LatencyBuckets),
		conversationTokens:   r.Gauge("mewcode_conversation_tokens", "Current conversation token estimate"),
		verificationRetriesG: r.Gauge("mewcode_verification_retries_current", "Current verification retry count"),
		panicsTotal:          r.Counter("mewcode_panics_total", "Total recovered panics by location", "location"),
		errorsTotal:          r.Counter("mewcode_errors_total", "Total errors by type", "type"),
		sessionSaveErrors:    r.Counter("mewcode_session_save_errors_total", "Session log save errors"),
		activeSessions:       r.Gauge("mewcode_active_sessions", "Active agent sessions"),
		activeTools:          r.Gauge("mewcode_active_tools", "Currently executing tools"),
		toolresultSpilled:    r.Counter("mewcode_toolresult_spilled_total", "Tool results spilled to disk"),
		subagentSpawnTotal:   r.Counter("mewcode_subagent_spawn_total", "Sub-agent spawns by mode", "mode"),
		subagentDuration:     r.Histogram("mewcode_subagent_duration_seconds", "Sub-agent execution duration", LatencyBuckets, "mode"),
		subagentFailureTotal: r.Counter("mewcode_subagent_failure_total", "Sub-agent failures by mode", "mode"),
		activeTeammates:      r.Gauge("mewcode_active_teammates", "Active teammate agents"),

		requestsTotal:          r.Counter("agent_requests_total", "Total user requests received", "outcome"),
		activeTasks:            r.Gauge("agent_active_tasks", "Currently active agent tasks"),
		taskDuration:           r.Histogram("agent_task_duration_seconds", "Agent task total duration in seconds", LatencyBuckets),
		stepsTotal:             r.Counter("agent_steps_total", "Total agent steps (turns) executed"),
		maxStepsReachedTotal:   r.Counter("agent_max_steps_reached_total", "Times max steps limit was reached"),
		taskTimeoutTotal:       r.Counter("agent_task_timeout_total", "Times agent task timed out"),
		llmErrorsTotal:         r.Counter("agent_llm_errors_total", "Total LLM errors by type", "type"),
		llmCostTotal:           r.Counter("agent_llm_cost_total", "Total LLM cost in USD by provider/model", "provider", "model"),
		toolErrorsTotal:        r.Counter("agent_tool_errors_total", "Total tool errors by name", "name"),
		filesModifiedTotal:     r.Counter("agent_files_modified_total", "Total files modified by file tools"),
		codeEditsTotal:         r.Counter("agent_code_edits_total", "Total code edit operations"),
		commandExecutionsTotal: r.Counter("agent_command_executions_total", "Total shell command executions"),
		buildTotal:             r.Counter("agent_build_total", "Total build command executions"),
		buildSuccessTotal:      r.Counter("agent_build_success_total", "Total successful builds"),
		testsTotal:             r.Counter("agent_tests_total", "Total test command executions"),
		testsPassedTotal:       r.Counter("agent_tests_passed_total", "Total test commands that passed"),
		testsFailedTotal:       r.Counter("agent_tests_failed_total", "Total test commands that failed"),
	}
}

func (m *Metrics) RecordAgentRun(outcome string) {
	if m == nil {
		return
	}
	m.agentRunTotal.Inc(outcome)
}

func (m *Metrics) RecordVerification(verdict string) {
	if m == nil {
		return
	}
	m.verificationResults.Inc(verdict)
}

func (m *Metrics) ObserveVerificationRetries(n int) {
	if m == nil {
		return
	}
	m.verificationRetries.Observe(float64(n))
}

func (m *Metrics) ObserveTurnsPerRun(n int) {
	if m == nil {
		return
	}
	m.turnsPerRun.Observe(float64(n))
}

func (m *Metrics) ObserveTurnLatency(d time.Duration) {
	if m == nil {
		return
	}
	m.turnLatency.Observe(d.Seconds())
}

func (m *Metrics) ObserveLLMLatency(provider, model string, d time.Duration) {
	if m == nil {
		return
	}
	m.llmLatency.Observe(d.Seconds(), provider, model)
}

func (m *Metrics) RecordLLMCall(provider, model, status string) {
	if m == nil {
		return
	}
	m.llmCallsTotal.Inc(provider, model, status)
}

func (m *Metrics) RecordTokenUsage(provider, model string, input, output, cacheRead, cacheCreate int) {
	if m == nil {
		return
	}
	m.llmTokensTotal.Add(float64(input), provider, model, "input")
	m.llmTokensTotal.Add(float64(output), provider, model, "output")
	m.llmTokensTotal.Add(float64(cacheRead), provider, model, "cache_read")
	m.llmTokensTotal.Add(float64(cacheCreate), provider, model, "cache_create")
}

func (m *Metrics) RecordLLMRetry(errType string) {
	if m == nil {
		return
	}
	m.llmRetryTotal.Inc(errType)
}

func (m *Metrics) RecordToolCall(name, status string) {
	if m == nil {
		return
	}
	m.toolCallsTotal.Inc(name, status)
}

func (m *Metrics) ObserveToolLatency(name string, d time.Duration) {
	if m == nil {
		return
	}
	m.toolLatency.Observe(d.Seconds(), name)
}

func (m *Metrics) RecordToolRejected(name, reason string) {
	if m == nil {
		return
	}
	m.toolRejectedTotal.Inc(name, reason)
}

func (m *Metrics) RecordCompaction(result string) {
	if m == nil {
		return
	}
	m.compactionRunsTotal.Inc(result)
}

func (m *Metrics) ObserveCompactionLatency(d time.Duration) {
	if m == nil {
		return
	}
	m.compactionLatency.Observe(d.Seconds())
}

func (m *Metrics) SetConversationTokens(n int) {
	if m == nil {
		return
	}
	m.conversationTokens.Set(float64(n))
}

func (m *Metrics) SetVerificationRetries(n int) {
	if m == nil {
		return
	}
	m.verificationRetriesG.Set(float64(n))
}

func (m *Metrics) RecordPanic(location string) {
	if m == nil {
		return
	}
	m.panicsTotal.Inc(location)
}

func (m *Metrics) RecordError(errType string) {
	if m == nil {
		return
	}
	m.errorsTotal.Inc(errType)
}

func (m *Metrics) RecordSessionSaveError() {
	if m == nil {
		return
	}
	m.sessionSaveErrors.Inc()
}

func (m *Metrics) IncActiveSessions() {
	if m == nil {
		return
	}
	m.activeSessions.Inc()
}

func (m *Metrics) DecActiveSessions() {
	if m == nil {
		return
	}
	m.activeSessions.Dec()
}

func (m *Metrics) SetActiveTools(n int) {
	if m == nil {
		return
	}
	m.activeTools.Set(float64(n))
}

func (m *Metrics) RecordToolresultSpilled() {
	if m == nil {
		return
	}
	m.toolresultSpilled.Inc()
}

func (m *Metrics) RecordSubagentSpawn(mode string) {
	if m == nil {
		return
	}
	m.subagentSpawnTotal.Inc(mode)
}

func (m *Metrics) ObserveSubagentDuration(mode string, d time.Duration) {
	if m == nil {
		return
	}
	m.subagentDuration.Observe(d.Seconds(), mode)
}

func (m *Metrics) RecordSubagentFailure(mode string) {
	if m == nil {
		return
	}
	m.subagentFailureTotal.Inc(mode)
}

func (m *Metrics) SetActiveTeammates(n int) {
	if m == nil {
		return
	}
	m.activeTeammates.Set(float64(n))
}

var Noop = NewMetrics(&noopRegistry{})

func NewNoopRegistry() Registry { return &noopRegistry{} }

func (m *Metrics) RecordRequest(outcome string) {
	if m == nil {
		return
	}
	m.requestsTotal.Inc(outcome)
}

func (m *Metrics) IncActiveTasks() {
	if m == nil {
		return
	}
	m.activeTasks.Inc()
}

func (m *Metrics) DecActiveTasks() {
	if m == nil {
		return
	}
	m.activeTasks.Dec()
}

func (m *Metrics) ObserveTaskDuration(d time.Duration) {
	if m == nil {
		return
	}
	m.taskDuration.Observe(d.Seconds())
}

func (m *Metrics) RecordStep() {
	if m == nil {
		return
	}
	m.stepsTotal.Inc()
}

func (m *Metrics) RecordMaxStepsReached() {
	if m == nil {
		return
	}
	m.maxStepsReachedTotal.Inc()
}

func (m *Metrics) RecordTaskTimeout() {
	if m == nil {
		return
	}
	m.taskTimeoutTotal.Inc()
}

func (m *Metrics) RecordLLMError(errType string) {
	if m == nil {
		return
	}
	m.llmErrorsTotal.Inc(errType)
}

func (m *Metrics) RecordLLMCost(provider, model string, cost float64) {
	if m == nil || cost <= 0 {
		return
	}
	m.llmCostTotal.Add(cost, provider, model)
}

func (m *Metrics) RecordToolError(name string) {
	if m == nil {
		return
	}
	m.toolErrorsTotal.Inc(name)
}

func (m *Metrics) RecordFileModified() {
	if m == nil {
		return
	}
	m.filesModifiedTotal.Inc()
}

func (m *Metrics) RecordCodeEdit() {
	if m == nil {
		return
	}
	m.codeEditsTotal.Inc()
}

func (m *Metrics) RecordCommandExecution() {
	if m == nil {
		return
	}
	m.commandExecutionsTotal.Inc()
}

func (m *Metrics) RecordBuild() {
	if m == nil {
		return
	}
	m.buildTotal.Inc()
}

func (m *Metrics) RecordBuildSuccess() {
	if m == nil {
		return
	}
	m.buildSuccessTotal.Inc()
}

func (m *Metrics) RecordTests() {
	if m == nil {
		return
	}
	m.testsTotal.Inc()
}

func (m *Metrics) RecordTestPassed() {
	if m == nil {
		return
	}
	m.testsPassedTotal.Inc()
}

func (m *Metrics) RecordTestFailed() {
	if m == nil {
		return
	}
	m.testsFailedTotal.Inc()
}
