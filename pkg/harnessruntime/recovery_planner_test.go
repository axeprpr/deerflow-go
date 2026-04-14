package harnessruntime

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestRecoveryPlannerAdvancesAttemptAndOutcome(t *testing.T) {
	planner := NewRecoveryPlanner()
	record := planner.NextRecord(RunRecord{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Attempt:     2,
		Status:      "error",
		Error:       "boom",
	}, 9, "resume-after-crash")

	if record.Attempt != 3 {
		t.Fatalf("Attempt = %d, want 3", record.Attempt)
	}
	if record.ResumeFromEvent != 9 || record.ResumeReason != "resume-after-crash" {
		t.Fatalf("resume = %+v", record)
	}
	if record.Status != "running" || record.Error != "" {
		t.Fatalf("record = %+v", record)
	}
	if record.Outcome.RunStatus != "running" || record.Outcome.Attempt != 3 {
		t.Fatalf("outcome = %+v", record.Outcome)
	}
}

func TestRecoveryPlannerBuildsResumedPlan(t *testing.T) {
	planner := NewRecoveryPlanner()
	plan := planner.ResumePlan(RunPlan{
		Model:     "model-1",
		AgentName: "lead_agent",
	}, RunRecord{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Attempt:     1,
	}, 4, "retry")

	if plan.RunID != "run-1" || plan.ThreadID != "thread-1" || plan.AssistantID != "lead_agent" {
		t.Fatalf("plan ids = %+v", plan)
	}
	if plan.Attempt != 2 || plan.ResumeFromEvent != 4 || plan.ResumeReason != "retry" {
		t.Fatalf("plan recovery = %+v", plan)
	}
}

func TestRecoveryPlannerCarriesLiveTaskStateIntoResumedOutcome(t *testing.T) {
	threads := NewInMemoryThreadStateStore()
	taskState := harness.TaskState{
		Items: []harness.TaskItem{
			{ID: "task-1", Text: "inspect repo", Status: harness.TaskStatusInProgress},
		},
		ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
	}
	threads.SetThreadMetadata("thread-1", DefaultTaskStateMetadataKey, taskState.Value())
	planner := NewRecoveryPlannerWithThreads(threads)

	record := planner.NextRecord(RunRecord{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Attempt:     1,
		Status:      "error",
	}, 3, "resume-after-crash")

	if record.Outcome.RunStatus != "running" {
		t.Fatalf("Outcome = %+v", record.Outcome)
	}
	if len(record.Outcome.TaskState.Items) != 1 || record.Outcome.TaskState.Items[0].ID != "task-1" {
		t.Fatalf("TaskState = %+v", record.Outcome.TaskState)
	}
	if len(record.Outcome.TaskLifecycle.ExpectedArtifacts) != 1 || record.Outcome.TaskLifecycle.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("TaskLifecycle = %+v", record.Outcome.TaskLifecycle)
	}
}
