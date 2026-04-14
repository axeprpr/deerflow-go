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
