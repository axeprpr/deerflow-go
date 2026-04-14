package harnessruntime

import "testing"

func TestRunStateServiceReconcileLiveClaimsUnownedRun(t *testing.T) {
	t.Parallel()

	threads := NewInMemoryThreadStateStore()
	service := NewRunStateService(workerRunStateRuntime{threads: threads})
	record := service.ReconcileLive(RunRecord{
		RunID:    "run-a",
		ThreadID: "thread-1",
		Attempt:  2,
		Status:   "running",
		Outcome: RunOutcomeDescriptor{
			RunStatus: "running",
			TaskLifecycle: TaskLifecycleDescriptor{
				Status: "running",
			},
		},
	})

	if record.Outcome.Attempt != 2 {
		t.Fatalf("outcome attempt = %d, want 2", record.Outcome.Attempt)
	}
	state, ok := threads.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("thread state missing after reconcile live")
	}
	if got := metadataRunID(state.Metadata[DefaultActiveRunMetadataKey]); got != "run-a" {
		t.Fatalf("active run id = %q, want run-a", got)
	}
	if got := state.Status; got != "busy" {
		t.Fatalf("thread status = %q, want busy", got)
	}
	lifecycle, ok := ParseTaskLifecycle(state.Metadata[DefaultTaskLifecycleMetadataKey])
	if !ok || lifecycle.Status != "running" {
		t.Fatalf("task lifecycle = %#v", state.Metadata[DefaultTaskLifecycleMetadataKey])
	}
}

func TestRunStateServiceReconcileLiveDoesNotStealOwnedRun(t *testing.T) {
	t.Parallel()

	threads := NewInMemoryThreadStateStore()
	threads.MarkThreadStatus("thread-1", "busy")
	threads.SetThreadMetadata("thread-1", DefaultRunIDMetadataKey, "run-b")
	threads.SetThreadMetadata("thread-1", DefaultActiveRunMetadataKey, "run-b")
	threads.SetThreadMetadata("thread-1", DefaultTaskLifecycleMetadataKey, TaskLifecycleDescriptor{
		Status: "running",
	}.Value())

	service := NewRunStateService(workerRunStateRuntime{threads: threads})
	service.ReconcileLive(RunRecord{
		RunID:    "run-a",
		ThreadID: "thread-1",
		Attempt:  1,
		Status:   "running",
		Outcome: RunOutcomeDescriptor{
			RunStatus: "running",
			TaskLifecycle: TaskLifecycleDescriptor{
				Status: "running",
			},
		},
	})

	state, ok := threads.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("thread state missing after reconcile live")
	}
	if got := metadataRunID(state.Metadata[DefaultActiveRunMetadataKey]); got != "run-b" {
		t.Fatalf("active run id = %q, want run-b", got)
	}
	if got := state.Status; got != "busy" {
		t.Fatalf("thread status = %q, want busy", got)
	}
	lifecycle, ok := ParseTaskLifecycle(state.Metadata[DefaultTaskLifecycleMetadataKey])
	if !ok || lifecycle.Status != "running" {
		t.Fatalf("task lifecycle = %#v", state.Metadata[DefaultTaskLifecycleMetadataKey])
	}
}
