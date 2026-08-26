package prompt

import "testing"

func TestBuildExecutionModeReminder(t *testing.T) {
	if got := BuildExecutionModeReminder("plan_and_execute"); got == "" {
		t.Fatal("plan mode reminder should be present")
	}
	if got := BuildExecutionModeReminder("react"); got == "" {
		t.Fatal("react mode reminder should be present")
	}
}
