package experiments

import (
	"context"
	"testing"
)

func TestRunProducesFiveExperimentResults(t *testing.T) {
	report, err := Run(context.Background(), "../..")
	if err != nil {
		t.Fatal(err)
	}
	if report.Tasks.Tasks != 20 || report.Tasks.SuccessRate < 0.8 {
		t.Fatalf("task baseline below target: %+v", report.Tasks)
	}
	if report.Recovery.ToolSuccessRate < 95 || report.Recovery.AutoRecoveryRate < 80 {
		t.Fatalf("recovery baseline below target: %+v", report.Recovery)
	}
	if report.Modes.LongTaskRate < 80 || !report.Modes.PauseRetryResume {
		t.Fatalf("mode experiment failed: %+v", report.Modes)
	}
	if !report.Compaction.BoundaryPersisted || !report.Compaction.InfoRetained || report.Compaction.ReductionPercent < 30 {
		t.Fatalf("compaction experiment failed: %+v", report.Compaction)
	}
	if report.Retrieval.Queries < 30 || report.Retrieval.Hybrid.Top5Accuracy <= 0 {
		t.Fatalf("retrieval experiment failed: %+v", report.Retrieval)
	}
}
