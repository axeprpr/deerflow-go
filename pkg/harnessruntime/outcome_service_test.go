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
}
