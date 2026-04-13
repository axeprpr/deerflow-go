package harnessruntime

import "testing"

func TestSQLiteRunSnapshotStorePersistsSnapshots(t *testing.T) {
	store, err := NewSQLiteRunSnapshotStore(t.TempDir() + "/snapshots.sqlite3")
	if err != nil {
		t.Fatalf("NewSQLiteRunSnapshotStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	store.SaveRunSnapshot(RunSnapshot{
		Record: RunRecord{RunID: "run-1", ThreadID: "thread-1", Status: "success"},
		Events: []RunEvent{{ID: "run-1:1", Event: "end", RunID: "run-1", ThreadID: "thread-1"}},
	})

	loaded, ok := store.LoadRunSnapshot("run-1")
	if !ok {
		t.Fatal("LoadRunSnapshot() = false, want true")
	}
	if loaded.Record.Status != "success" || len(loaded.Events) != 1 {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestSQLiteRunEventStorePersistsEvents(t *testing.T) {
	store, err := NewSQLiteRunEventStore(t.TempDir() + "/events.sqlite3")
	if err != nil {
		t.Fatalf("NewSQLiteRunEventStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	store.AppendRunEvent("run-1", RunEvent{ID: "run-1:1", Event: "updates"})
	store.AppendRunEvent("run-1", RunEvent{ID: "run-1:2", Event: "end"})

	events := store.LoadRunEvents("run-1")
	if len(events) != 2 || events[1].Event != "end" {
		t.Fatalf("LoadRunEvents() = %#v", events)
	}
}

func TestSQLiteThreadStateStorePersistsState(t *testing.T) {
	store, err := NewSQLiteThreadStateStore(t.TempDir() + "/threads.sqlite3")
	if err != nil {
		t.Fatalf("NewSQLiteThreadStateStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	store.MarkThreadStatus("thread-1", "busy")
	store.SetThreadMetadata("thread-1", "assistant_id", "lead_agent")

	state, ok := store.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("LoadThreadRuntimeState() = false, want true")
	}
	if state.Status != "busy" || state.Metadata["assistant_id"] != "lead_agent" {
		t.Fatalf("state = %#v", state)
	}
}
