package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewcode/internal/conversation"
	"mewcode/internal/filehistory"
	"mewcode/internal/llm"
	"mewcode/internal/tools"
)

func newAgentWithHistory(t *testing.T) (*Agent, *filehistory.History, string) {
	t.Helper()
	dir := t.TempDir()
	fh := filehistory.New(dir, "test-session")
	ag := New(&mockClient{responses: nil}, tools.NewRegistry(), "anthropic")
	ag.FileHistory = fh
	ag.WorkDir = dir
	return ag, fh, dir
}

func TestHandleVerificationFailure_SoftRetryNoRewind(t *testing.T) {
	ag, fh, dir := newAgentWithHistory(t)
	fpath := filepath.Join(dir, "target.go")
	original := "package main\n"
	if err := os.WriteFile(fpath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	fh.TrackEdit(fpath)
	fh.MakeSnapshot(0, "pre-task")
	ag.preTaskSnapshot = len(fh.GetSnapshots()) - 1

	modified := original + "// broken\n"
	if err := os.WriteFile(fpath, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}

	ag.verifyRetries = 0
	ag.verifyRetries++
	conv := conversation.NewManager()
	ag.handleVerificationFailure(conv, "fail")

	data, _ := os.ReadFile(fpath)
	if string(data) != modified {
		t.Error("soft retry (1st) must NOT rewind files")
	}
}

func TestHandleVerificationFailure_SecondRetryRewinds(t *testing.T) {
	ag, fh, dir := newAgentWithHistory(t)
	fpath := filepath.Join(dir, "target.go")
	original := "package main\n"
	if err := os.WriteFile(fpath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	fh.TrackEdit(fpath)
	fh.MakeSnapshot(0, "pre-task")
	ag.preTaskSnapshot = len(fh.GetSnapshots()) - 1

	modified := original + "// broken\n"
	if err := os.WriteFile(fpath, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}

	ag.verifyRetries = 2
	conv := conversation.NewManager()
	ag.handleVerificationFailure(conv, "fail again")

	data, _ := os.ReadFile(fpath)
	if string(data) != original {
		t.Errorf("2nd retry must rewind to pre-task; got %q, want %q", string(data), original)
	}
}

func TestHandleVerificationFailure_RewindDeletesNewFile(t *testing.T) {
	ag, fh, dir := newAgentWithHistory(t)
	fpath := filepath.Join(dir, "newfile.go")
	// TrackEdit before the file exists so the pre-task snapshot records the path
	// with no backup (signals "did not exist" -> delete on rewind).
	fh.TrackEdit(fpath)
	fh.MakeSnapshot(0, "pre-task")
	ag.preTaskSnapshot = len(fh.GetSnapshots()) - 1

	if err := os.WriteFile(fpath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	ag.verifyRetries = 2
	conv := conversation.NewManager()
	ag.handleVerificationFailure(conv, "fail")

	if _, err := os.Stat(fpath); !os.IsNotExist(err) {
		t.Error("rewind should delete file that did not exist at pre-task snapshot")
	}
}

func TestVerificationGate_IntegrationRewindsOnSecondFail(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "app.go")
	original := "package main\n"
	if err := os.WriteFile(fpath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	fh := filehistory.New(dir, "integ-session")

	writeTool := &fileWriteTool{path: fpath, content: "package main\n// broken\n", fh: fh}
	reg := tools.NewRegistry()
	reg.Register(writeTool)

	r1 := []llm.StreamEvent{
		llm.ToolCallStart{ToolName: "WriteFile", ToolID: "w1"},
		llm.ToolCallComplete{ToolID: "w1", ToolName: "WriteFile", Arguments: map[string]any{"file_path": fpath}},
		llm.ToolCallStart{ToolName: "WriteFile", ToolID: "w2"},
		llm.ToolCallComplete{ToolID: "w2", ToolName: "WriteFile", Arguments: map[string]any{"file_path": fpath}},
		llm.ToolCallStart{ToolName: "WriteFile", ToolID: "w3"},
		llm.ToolCallComplete{ToolID: "w3", ToolName: "WriteFile", Arguments: map[string]any{"file_path": fpath}},
		llm.StreamEnd{StopReason: "tool_use"},
	}
	r2 := textRound("done")
	r3 := textRound("fixed")
	r4 := textRound("again")
	client := &mockClient{responses: [][]llm.StreamEvent{r1, r2, r3, r4}}
	ag := New(client, reg, "anthropic")
	ag.FileHistory = fh
	ag.WorkDir = dir

	calls := 0
	ag.VerificationGate = func(ctx context.Context, conv *conversation.Manager) (Verdict, string, error) {
		calls++
		return VerdictFail, "broken", nil
	}

	conv := conversation.NewManager()
	conv.AddUserMessage("edit app.go")
	collectEvents(ag.Run(context.Background(), conv))

	data, _ := os.ReadFile(fpath)
	if string(data) != original {
		t.Errorf("after 2 failed retries file should be rolled back to original; got %q, want %q", string(data), original)
	}
	if calls != maxVerifyRetries+1 {
		t.Errorf("gate called %d times, want %d", calls, maxVerifyRetries+1)
	}
}

type fileWriteTool struct {
	path    string
	content string
	fh      *filehistory.History
}

func (t *fileWriteTool) Name() string                 { return "WriteFile" }
func (t *fileWriteTool) Description() string          { return "mock write" }
func (t *fileWriteTool) Category() tools.ToolCategory { return tools.CategoryWrite }
func (t *fileWriteTool) Schema() map[string]any {
	return map[string]any{
		"name": "WriteFile", "description": "mock",
		"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
	}
}
func (t *fileWriteTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	if p, ok := args["file_path"].(string); ok {
		if t.fh != nil {
			t.fh.TrackEdit(p)
		}
		_ = os.WriteFile(p, []byte(t.content), 0o644)
	}
	return tools.ToolResult{Output: "written"}
}
