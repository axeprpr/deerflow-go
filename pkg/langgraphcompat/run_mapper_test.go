package langgraphcompat

import (
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestRunMapperRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	record := harnessruntime.RunRecord{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Status:      "running",
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "running"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	run := runFromRecord(record)
	roundTrip := runRecordFromRun(run)
	if roundTrip.RunID != record.RunID ||
		roundTrip.ThreadID != record.ThreadID ||
		roundTrip.AssistantID != record.AssistantID ||
		roundTrip.Status != record.Status ||
		roundTrip.Outcome.RunStatus != record.Outcome.RunStatus ||
		!roundTrip.CreatedAt.Equal(record.CreatedAt) ||
		!roundTrip.UpdatedAt.Equal(record.UpdatedAt) {
		t.Fatalf("roundTrip = %+v want %+v", roundTrip, record)
	}
}

func TestApplyRunRecordMutatesCompatRun(t *testing.T) {
	run := &Run{RunID: "old", ThreadID: "thread-old", Status: "queued"}
	record := harnessruntime.RunRecord{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Status:      "success",
		Outcome:     harnessruntime.RunOutcomeDescriptor{RunStatus: "success"},
	}

	applyRunRecord(run, record)
	if run.RunID != "run-1" || run.ThreadID != "thread-1" || run.Status != "success" || run.Outcome.RunStatus != "success" {
		t.Fatalf("run = %+v", run)
	}
}

func TestRunEventMapperRoundTripPreservesOutcome(t *testing.T) {
	event := harnessruntime.RunEvent{
		ID:       "run-1:3",
		Event:    "end",
		RunID:    "run-1",
		ThreadID: "thread-1",
		Outcome:  harnessruntime.RunOutcomeDescriptor{RunStatus: "success"},
	}

	stream := streamEventFromRuntimeEvent(event)
	roundTrip := runtimeEventFromStreamEvent(stream)
	if roundTrip.Outcome.RunStatus != "success" {
		t.Fatalf("roundTrip outcome = %+v", roundTrip.Outcome)
	}
}

func TestRunFromRecordBindsOutcomeDescriptorTaskFields(t *testing.T) {
	record := harnessruntime.RunRecord{
		RunID:           "run-1",
		ThreadID:        "thread-1",
		AssistantID:     "lead_agent",
		Attempt:         2,
		ResumeFromEvent: 5,
		ResumeReason:    "replay",
		Status:          "running",
		Outcome: harnessruntime.RunOutcomeDescriptor{
			RunStatus: "running",
			TaskLifecycle: harnessruntime.TaskLifecycleDescriptor{
				Status:            "running",
				PendingTasks:      []string{"delegate research"},
				ExpectedArtifacts: []string{"/mnt/user-data/outputs/report.md"},
			},
		},
	}

	run := runFromRecord(record)
	if run.Outcome.Attempt != 2 || run.Outcome.ResumeFromEvent != 5 || run.Outcome.ResumeReason != "replay" {
		t.Fatalf("outcome recovery = %+v", run.Outcome)
	}
	if len(run.Outcome.PendingTasks) != 1 || run.Outcome.PendingTasks[0] != "delegate research" {
		t.Fatalf("pending tasks = %#v", run.Outcome.PendingTasks)
	}
	if len(run.Outcome.ExpectedArtifacts) != 1 || run.Outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("expected artifacts = %#v", run.Outcome.ExpectedArtifacts)
	}
}

func TestRunFromSnapshotNormalizesEventOutcomeFromRecord(t *testing.T) {
	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	snapshot := harnessruntime.RunSnapshot{
		Record: harnessruntime.RunRecord{
			RunID:           "run-1",
			ThreadID:        "thread-1",
			AssistantID:     "lead_agent",
			Attempt:         4,
			ResumeFromEvent: 9,
			ResumeReason:    "replay",
			Status:          "running",
			Outcome: harnessruntime.RunOutcomeDescriptor{
				RunStatus: "running",
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		Events: []harnessruntime.RunEvent{
			{
				ID:    "run-1:10",
				Event: "task_running",
				Outcome: harnessruntime.RunOutcomeDescriptor{
					RunStatus: "running",
					TaskLifecycle: harnessruntime.TaskLifecycleDescriptor{
						Status:            "running",
						PendingTasks:      []string{"delegate research"},
						ExpectedArtifacts: []string{"/mnt/user-data/outputs/report.md"},
					},
				},
			},
		},
	}

	run := runFromSnapshot(snapshot)
	if len(run.Events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(run.Events))
	}
	event := run.Events[0]
	if event.Attempt != 4 || event.ResumeFromEvent != 9 || event.ResumeReason != "replay" {
		t.Fatalf("event recovery = %+v", event)
	}
	if event.Outcome.Attempt != 4 || event.Outcome.ResumeFromEvent != 9 || event.Outcome.ResumeReason != "replay" {
		t.Fatalf("event outcome recovery = %+v", event.Outcome)
	}
	if len(event.Outcome.PendingTasks) != 1 || event.Outcome.PendingTasks[0] != "delegate research" {
		t.Fatalf("pending tasks = %#v", event.Outcome.PendingTasks)
	}
	if len(event.Outcome.ExpectedArtifacts) != 1 || event.Outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("expected artifacts = %#v", event.Outcome.ExpectedArtifacts)
	}
}
