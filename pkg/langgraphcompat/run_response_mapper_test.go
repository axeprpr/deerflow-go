package langgraphcompat

import (
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestRunRecordResponseIncludesRecoveryFieldsAndOutcome(t *testing.T) {
	now := time.Now().UTC()
	response := runRecordResponse(harnessruntime.RunRecord{
		RunID:           "run-1",
		ThreadID:        "thread-1",
		AssistantID:     "assistant-1",
		Attempt:         2,
		ResumeFromEvent: 9,
		ResumeReason:    "worker-retry",
		Status:          "interrupted",
		Error:           "worker disconnected",
		Outcome: harnessruntime.RunOutcomeDescriptor{
			RunStatus:         "interrupted",
			Interrupted:       true,
			Error:             "worker disconnected",
			PendingTasks:      []string{"collect metrics"},
			ExpectedArtifacts: []string{"/mnt/user-data/outputs/report.md"},
			TaskState: harness.TaskState{
				Items: []harness.TaskItem{
					{Text: "collect metrics", Status: harness.TaskStatusInProgress},
				},
				ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
			},
			TaskLifecycle: harnessruntime.TaskLifecycleDescriptor{
				Status:            "running",
				PendingTasks:      []string{"collect metrics"},
				ExpectedArtifacts: []string{"/mnt/user-data/outputs/report.md"},
			},
		},
		CreatedAt: now.Add(-time.Second),
		UpdatedAt: now,
	})

	if got, _ := response["attempt"].(int); got != 2 {
		t.Fatalf("attempt=%v want=2", response["attempt"])
	}
	if got, _ := response["resume_from_event"].(int); got != 9 {
		t.Fatalf("resume_from_event=%v want=9", response["resume_from_event"])
	}
	if got, _ := response["resume_reason"].(string); got != "worker-retry" {
		t.Fatalf("resume_reason=%q", got)
	}

	outcome, _ := response["outcome"].(map[string]any)
	if outcome == nil {
		t.Fatalf("outcome=%T want map", response["outcome"])
	}
	if got, _ := outcome["run_status"].(string); got != "interrupted" {
		t.Fatalf("outcome.run_status=%q", got)
	}
	if got, _ := outcome["attempt"].(int); got != 2 {
		t.Fatalf("outcome.attempt=%v want=2", outcome["attempt"])
	}
	if got, _ := outcome["resume_from_event"].(int); got != 9 {
		t.Fatalf("outcome.resume_from_event=%v want=9", outcome["resume_from_event"])
	}
	if got, _ := outcome["resume_reason"].(string); got != "worker-retry" {
		t.Fatalf("outcome.resume_reason=%q", got)
	}
	if taskState, ok := outcome["task_state"].(map[string]any); !ok || taskState == nil {
		t.Fatalf("outcome.task_state=%T", outcome["task_state"])
	}
	if lifecycle, ok := outcome["task_lifecycle"].(map[string]any); !ok || lifecycle == nil {
		t.Fatalf("outcome.task_lifecycle=%T", outcome["task_lifecycle"])
	}
}

func TestRunRecordResponseOmitsEmptyRecoveryFields(t *testing.T) {
	now := time.Now().UTC()
	response := runRecordResponse(harnessruntime.RunRecord{
		RunID:       "run-2",
		ThreadID:    "thread-2",
		AssistantID: "assistant-2",
		Status:      "success",
		CreatedAt:   now.Add(-time.Second),
		UpdatedAt:   now,
	})

	if _, ok := response["attempt"]; ok {
		t.Fatalf("attempt should be omitted: %#v", response["attempt"])
	}
	if _, ok := response["resume_from_event"]; ok {
		t.Fatalf("resume_from_event should be omitted: %#v", response["resume_from_event"])
	}
	if _, ok := response["resume_reason"]; ok {
		t.Fatalf("resume_reason should be omitted: %#v", response["resume_reason"])
	}
	if _, ok := response["outcome"]; ok {
		t.Fatalf("outcome should be omitted: %#v", response["outcome"])
	}
}
