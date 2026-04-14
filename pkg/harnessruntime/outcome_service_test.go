package harnessruntime

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestOutcomeServiceDescribeLiveRunningUsesThreadState(t *testing.T) {
	store := NewInMemoryThreadStateStore()
	taskState := harness.TaskState{
		Items: []harness.TaskItem{
			{ID: "task-1", Text: "inspect repo", Status: harness.TaskStatusInProgress},
		},
		ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
	}
	store.SetThreadMetadata("thread-1", DefaultTaskStateMetadataKey, taskState.Value())
	store.SetThreadMetadata("thread-1", DefaultTaskLifecycleMetadataKey, NewTaskLifecycleService().Describe(
		RunOutcome{RunStatus: "running"},
		taskState,
		false,
	).Value())

	outcome := NewOutcomeService().DescribeLiveRunning(RunRecord{
		ThreadID: "thread-1",
		Attempt:  2,
	}, store)

	if outcome.RunStatus != "running" {
		t.Fatalf("RunStatus = %q, want running", outcome.RunStatus)
	}
	if outcome.Attempt != 2 {
		t.Fatalf("Attempt = %d, want 2", outcome.Attempt)
	}
	if len(outcome.TaskState.Items) != 1 || outcome.TaskState.Items[0].ID != "task-1" {
		t.Fatalf("TaskState = %+v", outcome.TaskState)
	}
	if len(outcome.TaskLifecycle.ExpectedArtifacts) != 1 || outcome.TaskLifecycle.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("TaskLifecycle = %+v", outcome.TaskLifecycle)
	}
	if len(outcome.ExpectedArtifacts) != 1 || outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("ExpectedArtifacts = %#v", outcome.ExpectedArtifacts)
	}
}

func TestOutcomeServiceDescribeLiveRunningUsesLifecycleFallback(t *testing.T) {
	store := NewInMemoryThreadStateStore()
	store.SetThreadMetadata("thread-2", DefaultTaskLifecycleMetadataKey, TaskLifecycleDescriptor{
		Status:            "running",
		PendingTasks:      []string{"draft summary"},
		ExpectedArtifacts: []string{"/mnt/user-data/outputs/summary.md"},
	}.Value())

	outcome := NewOutcomeService().DescribeLiveRunning(RunRecord{
		ThreadID: "thread-2",
		Attempt:  1,
	}, store)

	if outcome.TaskState.IsZero() != true {
		t.Fatalf("TaskState = %+v, want zero", outcome.TaskState)
	}
	if len(outcome.PendingTasks) != 1 || outcome.PendingTasks[0] != "draft summary" {
		t.Fatalf("PendingTasks = %#v", outcome.PendingTasks)
	}
	if len(outcome.ExpectedArtifacts) != 1 || outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/summary.md" {
		t.Fatalf("ExpectedArtifacts = %#v", outcome.ExpectedArtifacts)
	}
	if outcome.TaskLifecycle.Status != "running" {
		t.Fatalf("TaskLifecycle = %+v", outcome.TaskLifecycle)
	}
}

func TestOutcomeServiceDescribeWithTaskStateBuildsLifecycle(t *testing.T) {
	taskState := harness.TaskState{
		Items: []harness.TaskItem{
			{ID: "task-1", Text: "inspect repo", Status: harness.TaskStatusInProgress},
		},
		ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
	}

	outcome := NewOutcomeService().DescribeWithTaskState(RunRecord{Attempt: 3}, RunOutcome{RunStatus: "interrupted", Interrupted: true}, "", taskState, true)

	if outcome.RunStatus != "interrupted" || !outcome.Interrupted {
		t.Fatalf("Outcome = %+v", outcome)
	}
	if outcome.Attempt != 3 {
		t.Fatalf("Attempt = %d, want 3", outcome.Attempt)
	}
	if outcome.TaskLifecycle.Status != "paused" {
		t.Fatalf("TaskLifecycle = %+v, want paused", outcome.TaskLifecycle)
	}
	if len(outcome.TaskState.Items) != 1 || outcome.TaskState.Items[0].ID != "task-1" {
		t.Fatalf("TaskState = %+v", outcome.TaskState)
	}
	if len(outcome.PendingTasks) != 1 || outcome.PendingTasks[0] != "inspect repo" {
		t.Fatalf("PendingTasks = %#v", outcome.PendingTasks)
	}
	if len(outcome.ExpectedArtifacts) != 1 || outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("ExpectedArtifacts = %#v", outcome.ExpectedArtifacts)
	}
}

func TestOutcomeServiceBindRecordBackfillsTaskDetailsFromLifecycle(t *testing.T) {
	descriptor := NewOutcomeService().BindRecord(RunRecord{
		Attempt:         4,
		ResumeFromEvent: 11,
		ResumeReason:    "resume-after-disconnect",
	}, RunOutcomeDescriptor{
		RunStatus: "incomplete",
		TaskLifecycle: TaskLifecycleDescriptor{
			Status:            "incomplete",
			PendingTasks:      []string{"verify artifact"},
			ExpectedArtifacts: []string{"/mnt/user-data/outputs/report.md"},
		},
	})

	if descriptor.Attempt != 4 || descriptor.ResumeFromEvent != 11 || descriptor.ResumeReason != "resume-after-disconnect" {
		t.Fatalf("record fields = %+v", descriptor)
	}
	if len(descriptor.PendingTasks) != 1 || descriptor.PendingTasks[0] != "verify artifact" {
		t.Fatalf("PendingTasks = %#v", descriptor.PendingTasks)
	}
	if len(descriptor.ExpectedArtifacts) != 1 || descriptor.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("ExpectedArtifacts = %#v", descriptor.ExpectedArtifacts)
	}
}
