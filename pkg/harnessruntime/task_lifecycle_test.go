package harnessruntime

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
)

func TestThreadTaskLifecycleTrackerTracksDelegatedTasks(t *testing.T) {
	t.Parallel()

	store := NewInMemoryThreadStateStore()
	tracker := NewThreadTaskLifecycleTracker(store, "thread-1")

	tracker.Observe(subagent.TaskEvent{Type: "task_running", Description: "delegate research"})
	state, ok := store.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("thread state missing after task_running")
	}
	lifecycle, ok := ParseTaskLifecycle(state.Metadata[DefaultTaskLifecycleMetadataKey])
	if !ok {
		t.Fatalf("task lifecycle=%#v", state.Metadata[DefaultTaskLifecycleMetadataKey])
	}
	if lifecycle.Status != "running" || len(lifecycle.PendingTasks) != 1 || lifecycle.PendingTasks[0] != "delegate research" {
		t.Fatalf("lifecycle after task_running=%+v", lifecycle)
	}
	taskState, ok := harness.ParseTaskState(state.Metadata[DefaultTaskStateMetadataKey])
	if !ok {
		t.Fatalf("task state=%#v", state.Metadata[DefaultTaskStateMetadataKey])
	}
	if len(taskState.Items) != 1 || taskState.Items[0].Status != harness.TaskStatusInProgress {
		t.Fatalf("task state after task_running=%+v", taskState)
	}

	tracker.Observe(subagent.TaskEvent{Type: "task_completed", Description: "delegate research"})
	state, _ = store.LoadThreadRuntimeState("thread-1")
	lifecycle, ok = ParseTaskLifecycle(state.Metadata[DefaultTaskLifecycleMetadataKey])
	if !ok {
		t.Fatalf("task lifecycle=%#v", state.Metadata[DefaultTaskLifecycleMetadataKey])
	}
	if lifecycle.Status != "running" || len(lifecycle.PendingTasks) != 0 {
		t.Fatalf("lifecycle after task_completed=%+v", lifecycle)
	}
	taskState, ok = harness.ParseTaskState(state.Metadata[DefaultTaskStateMetadataKey])
	if !ok {
		t.Fatalf("task state=%#v", state.Metadata[DefaultTaskStateMetadataKey])
	}
	if len(taskState.Items) != 1 || taskState.Items[0].Status != harness.TaskStatusCompleted {
		t.Fatalf("task state after task_completed=%+v", taskState)
	}
}

func TestThreadTaskLifecycleTrackerKeepsFailedDelegatedTasksPending(t *testing.T) {
	t.Parallel()

	store := NewInMemoryThreadStateStore()
	tracker := NewThreadTaskLifecycleTracker(store, "thread-1")

	tracker.Observe(subagent.TaskEvent{Type: "task_failed", Description: "draft layout"})
	state, ok := store.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("thread state missing after task_failed")
	}
	lifecycle, ok := ParseTaskLifecycle(state.Metadata[DefaultTaskLifecycleMetadataKey])
	if !ok {
		t.Fatalf("task lifecycle=%#v", state.Metadata[DefaultTaskLifecycleMetadataKey])
	}
	if lifecycle.Status != "running" || len(lifecycle.PendingTasks) != 1 || lifecycle.PendingTasks[0] != "draft layout" {
		t.Fatalf("lifecycle after task_failed=%+v", lifecycle)
	}
	taskState, ok := harness.ParseTaskState(state.Metadata[DefaultTaskStateMetadataKey])
	if !ok {
		t.Fatalf("task state=%#v", state.Metadata[DefaultTaskStateMetadataKey])
	}
	if len(taskState.Items) != 1 || taskState.Items[0].Status != harness.TaskStatusPending {
		t.Fatalf("task state after task_failed=%+v", taskState)
	}
}

func TestThreadTaskLifecycleTrackerRecomputesLifecycleFromTaskState(t *testing.T) {
	t.Parallel()

	store := NewInMemoryThreadStateStore()
	store.SetThreadMetadata("thread-1", DefaultTaskStateMetadataKey, harness.TaskState{
		Items: []harness.TaskItem{
			{Text: "draft report", Status: harness.TaskStatusCompleted},
		},
		ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
		VerifiedOutputs: []string{"/mnt/user-data/outputs/draft.md"},
	}.Value())

	tracker := NewThreadTaskLifecycleTracker(store, "thread-1")
	tracker.Observe(subagent.TaskEvent{Type: "task_running", Description: "delegate review"})

	state, ok := store.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("thread state missing after task_running")
	}
	lifecycle, ok := ParseTaskLifecycle(state.Metadata[DefaultTaskLifecycleMetadataKey])
	if !ok {
		t.Fatalf("task lifecycle=%#v", state.Metadata[DefaultTaskLifecycleMetadataKey])
	}
	if lifecycle.Status != "running" {
		t.Fatalf("lifecycle status=%q", lifecycle.Status)
	}
	if len(lifecycle.PendingTasks) != 1 || lifecycle.PendingTasks[0] != "delegate review" {
		t.Fatalf("pending tasks=%#v", lifecycle.PendingTasks)
	}
	if len(lifecycle.ExpectedArtifacts) != 1 || lifecycle.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("expected artifacts=%#v", lifecycle.ExpectedArtifacts)
	}
	if len(lifecycle.VerifiedArtifacts) != 1 || lifecycle.VerifiedArtifacts[0] != "/mnt/user-data/outputs/draft.md" {
		t.Fatalf("verified artifacts=%#v", lifecycle.VerifiedArtifacts)
	}
}
