package taskstate

import (
	"os"
	"testing"
)

func TestChooseModeUsesPlanForComplexWork(t *testing.T) {
	mode, complexity := ChooseMode("Refactor three files, run tests and build, then update the module")
	if mode != ModePlanAndExecute || !complexity.NeedsPlan {
		t.Fatalf("expected plan mode, got mode=%q complexity=%+v", mode, complexity)
	}
}

func TestChooseModeUsesReactForFocusedTask(t *testing.T) {
	mode, complexity := ChooseMode("Find the parser function and explain its error")
	if mode != ModeReAct || complexity.NeedsPlan {
		t.Fatalf("expected react mode, got mode=%q complexity=%+v", mode, complexity)
	}
}

func TestStateTransitionsAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s := New("task-1", "fix one file")
	s.Start()
	s.SetCheckpoint(2, "tests")
	s.Pause("user interrupted")
	if s.Status != StatusPaused || s.Attempt != 1 || s.Step != 2 {
		t.Fatalf("unexpected paused state: %+v", s)
	}
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusPaused || loaded.LastError != "user interrupted" || loaded.Checkpoint != "tests" {
		t.Fatalf("state did not round-trip: %+v", loaded)
	}
	loaded.Retry()
	loaded.Complete()
	if loaded.Status != StatusCompleted || loaded.CompletedAt.IsZero() {
		t.Fatalf("expected completed state: %+v", loaded)
	}
	if _, err := os.Stat(Path(dir, "task-1")); err != nil {
		t.Fatal(err)
	}
}
