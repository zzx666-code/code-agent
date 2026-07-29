package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"mewcode/internal/conversation"
	"mewcode/internal/llm"
	"mewcode/internal/tools"
)

// TestAgentToolCallChain: 多轮工具调用链路 - LLM 调工具->拿到结果->再调工具->最终回答
func TestAgentToolCallChain(t *testing.T) {
	client := &mockClient{responses: [][]llm.StreamEvent{
		{
			llm.ToolCallStart{ToolName: "ReadFile", ToolID: "t1"},
			llm.ToolCallComplete{ToolID: "t1", ToolName: "ReadFile", Arguments: map[string]any{"file_path": "/tmp/a"}},
			llm.StreamEnd{StopReason: "tool_use"},
		},
		{
			llm.ToolCallStart{ToolName: "Grep", ToolID: "t2"},
			llm.ToolCallComplete{ToolID: "t2", ToolName: "Grep", Arguments: map[string]any{"pattern": "foo"}},
			llm.StreamEnd{StopReason: "tool_use"},
		},
		{
			llm.TextDelta{Text: "Based on reading and grepping, here is the answer."},
			llm.StreamEnd{StopReason: "end_turn"},
		},
	}}
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "ReadFile", result: "file content here"})
	reg.Register(&mockTool{name: "Grep", result: "grep match: foo at line 5"})
	ag := New(client, reg, "anthropic")
	conv := conversation.NewManager()
	text, events := runConversationRound(ag, conv, "find foo")
	if !strings.Contains(text, "answer") {
		t.Errorf("expected final answer, got %q", text)
	}
	trs := getToolResults(events)
	if len(trs) != 2 {
		t.Errorf("expected 2 tool results, got %d", len(trs))
	}
	if trs[0].ToolName != "ReadFile" || trs[1].ToolName != "Grep" {
		t.Errorf("tool call order wrong: %s then %s", trs[0].ToolName, trs[1].ToolName)
	}
}

// TestAgentParallelToolCalls: 单轮内并行调用多个工具
func TestAgentParallelToolCalls(t *testing.T) {
	client := &mockClient{responses: [][]llm.StreamEvent{{
		llm.ToolCallStart{ToolName: "ReadFile", ToolID: "t1"},
		llm.ToolCallComplete{ToolID: "t1", ToolName: "ReadFile", Arguments: map[string]any{"file_path": "/tmp/a"}},
		llm.ToolCallStart{ToolName: "Glob", ToolID: "t2"},
		llm.ToolCallComplete{ToolID: "t2", ToolName: "Glob", Arguments: map[string]any{"pattern": "*.go"}},
		llm.StreamEnd{StopReason: "tool_use"},
	}, {
		llm.TextDelta{Text: "done"},
		llm.StreamEnd{StopReason: "end_turn"},
	}}}
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "ReadFile", result: "content"})
	reg.Register(&mockTool{name: "Glob", result: "a.go\nb.go"})
	ag := New(client, reg, "anthropic")
	conv := conversation.NewManager()
	_, events := runConversationRound(ag, conv, "do both")
	trs := getToolResults(events)
	if len(trs) != 2 {
		t.Errorf("expected 2 parallel tool results, got %d", len(trs))
	}
}

// TestAgentErrorRecovery: 工具返回错误后 agent 能继续
func TestAgentErrorRecovery(t *testing.T) {
	client := &mockClient{responses: [][]llm.StreamEvent{
		{
			llm.ToolCallStart{ToolName: "ReadFile", ToolID: "t1"},
			llm.ToolCallComplete{ToolID: "t1", ToolName: "ReadFile", Arguments: map[string]any{"file_path": "/missing"}},
			llm.StreamEnd{StopReason: "tool_use"},
		},
		{
			llm.TextDelta{Text: "File not found, trying alternative."},
			llm.StreamEnd{StopReason: "end_turn"},
		},
	}}
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "ReadFile", result: "Error: file not found"})
	ag := New(client, reg, "anthropic")
	conv := conversation.NewManager()
	text, _ := runConversationRound(ag, conv, "read missing file")
	if !strings.Contains(text, "alternative") {
		t.Errorf("expected recovery text, got %q", text)
	}
}

// TestAgentContextCancellation: ctx 取消后 agent 停止
func TestAgentContextCancellation(t *testing.T) {
	client := &mockClient{responses: [][]llm.StreamEvent{
		{llm.StreamEnd{StopReason: "end_turn"}},
	}}
	ag := New(client, tools.NewRegistry(), "anthropic")
	conv := conversation.NewManager()
	conv.AddUserMessage("hi")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := ag.Run(ctx, conv)
	// 取消后 channel 应该关闭（不阻塞），不强制要求有事件
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Error("agent did not stop after context cancellation")
	}
}

// TestStreamingExecutorToolCallDelta: 工具调用增量传输
func TestStreamingExecutorToolCallDelta(t *testing.T) {
	client := &mockClient{responses: [][]llm.StreamEvent{{
		llm.ToolCallStart{ToolName: "Bash", ToolID: "t1"},
		llm.ToolCallDelta{Text: `{"comm`},
		llm.ToolCallDelta{Text: `and":"ls"}`},
		llm.ToolCallComplete{ToolID: "t1", ToolName: "Bash", Arguments: map[string]any{"command": "ls"}},
		llm.StreamEnd{StopReason: "tool_use"},
	}, {
		llm.TextDelta{Text: "listed files"},
		llm.StreamEnd{StopReason: "end_turn"},
	}}}
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "Bash", result: "file1\nfile2"})
	ag := New(client, reg, "anthropic")
	conv := conversation.NewManager()
	text, events := runConversationRound(ag, conv, "list files")
	if !strings.Contains(text, "listed") {
		t.Errorf("got %q", text)
	}
	deltas := 0
	for _, ev := range events {
		if _, ok := ev.(ToolUseEvent); ok {
			deltas++
		}
	}
	if deltas == 0 {
		t.Error("expected tool use events")
	}
}

// --- 性能基准 ---

// BenchmarkAgentSimpleResponse: 单轮对话（无工具）的 Agent 循环性能
func BenchmarkAgentSimpleResponse(b *testing.B) {
	client := &mockClient{responses: [][]llm.StreamEvent{{
		llm.TextDelta{Text: "Hello world this is a response."},
		llm.StreamEnd{StopReason: "end_turn"},
	}}}
	ag := New(client, tools.NewRegistry(), "anthropic")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv := conversation.NewManager()
		conv.AddUserMessage("hi")
		collectEvents(ag.Run(context.Background(), conv))
	}
}

// BenchmarkAgentToolCallLoop: 带工具调用的 Agent 循环性能
func BenchmarkAgentToolCallLoop(b *testing.B) {
	client := &mockClient{responses: [][]llm.StreamEvent{
		{
			llm.ToolCallStart{ToolName: "ReadFile", ToolID: "t1"},
			llm.ToolCallComplete{ToolID: "t1", ToolName: "ReadFile", Arguments: map[string]any{"file_path": "/tmp/x"}},
			llm.StreamEnd{StopReason: "tool_use"},
		},
		{
			llm.TextDelta{Text: "done"},
			llm.StreamEnd{StopReason: "end_turn"},
		},
	}}
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "ReadFile", result: "content"})
	ag := New(client, reg, "anthropic")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv := conversation.NewManager()
		conv.AddUserMessage("read it")
		collectEvents(ag.Run(context.Background(), conv))
	}
}

// BenchmarkAgentMultiToolChain: 多轮工具调用链性能
func BenchmarkAgentMultiToolChain(b *testing.B) {
	client := &mockClient{responses: [][]llm.StreamEvent{
		{
			llm.ToolCallStart{ToolName: "ReadFile", ToolID: "t1"},
			llm.ToolCallComplete{ToolID: "t1", ToolName: "ReadFile", Arguments: map[string]any{}},
			llm.StreamEnd{StopReason: "tool_use"},
		},
		{
			llm.ToolCallStart{ToolName: "Grep", ToolID: "t2"},
			llm.ToolCallComplete{ToolID: "t2", ToolName: "Grep", Arguments: map[string]any{}},
			llm.StreamEnd{StopReason: "tool_use"},
		},
		{
			llm.TextDelta{Text: "final answer"},
			llm.StreamEnd{StopReason: "end_turn"},
		},
	}}
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "ReadFile", result: "content"})
	reg.Register(&mockTool{name: "Grep", result: "match"})
	ag := New(client, reg, "anthropic")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv := conversation.NewManager()
		conv.AddUserMessage("search")
		collectEvents(ag.Run(context.Background(), conv))
	}
}

// BenchmarkAgentLongConversation: 长对话上下文的 Agent 循环性能
func BenchmarkAgentLongConversation(b *testing.B) {
	client := &mockClient{responses: [][]llm.StreamEvent{{
		llm.TextDelta{Text: "response"},
		llm.StreamEnd{StopReason: "end_turn"},
	}}}
	ag := New(client, tools.NewRegistry(), "anthropic")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv := conversation.NewManager()
		for j := 0; j < 20; j++ {
			conv.AddUserMessage(fmt.Sprintf("message %d with some content", j))
			conv.AddAssistantMessage(fmt.Sprintf("response %d", j))
		}
		conv.AddUserMessage("final question")
		collectEvents(ag.Run(context.Background(), conv))
	}
}

// 防止 unused import
var _ = time.Second
