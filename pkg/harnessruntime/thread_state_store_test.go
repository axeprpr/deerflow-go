package harnessruntime

import "testing"

func TestInMemoryThreadStateStoreMutations(t *testing.T) {
	store := NewInMemoryThreadStateStore()
	store.MarkThreadStatus("thread-1", "busy")
	store.SetThreadMetadata("thread-1", "assistant_id", "lead_agent")

	state, ok := store.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("LoadThreadRuntimeState() = false, want true")
	}
	if state.Status != "busy" {
		t.Fatalf("status = %q, want busy", state.Status)
	}
	if state.Metadata["assistant_id"] != "lead_agent" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestJSONFileThreadStateStorePersistsState(t *testing.T) {
	store := NewJSONFileThreadStateStore(t.TempDir())
	store.MarkThreadStatus("thread-1", "idle")
	store.SetThreadMetadata("thread-1", "run_id", "run-1")

	state, ok := store.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("LoadThreadRuntimeState() = false, want true")
	}
	if state.Metadata["run_id"] != "run-1" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

