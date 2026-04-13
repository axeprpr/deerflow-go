package harnessruntime

import (
	"testing"
	"time"
)

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

func TestSQLiteRunEventStoreSubscribeRunEvents(t *testing.T) {
	store, err := NewSQLiteRunEventStore(t.TempDir() + "/events.sqlite3")
	if err != nil {
		t.Fatalf("NewSQLiteRunEventStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	sub, unsubscribe := store.SubscribeRunEvents("run-1", 4)
	defer unsubscribe()

	store.AppendRunEvent("run-1", RunEvent{ID: "run-1:1", Event: "chunk"})
	store.AppendRunEvent("run-1", RunEvent{ID: "run-1:2", Event: "end"})

	var events []RunEvent
	deadline := time.After(2 * time.Second)
	for len(events) < 2 {
		select {
		case event, ok := <-sub:
			if !ok {
				t.Fatal("subscription closed before events arrived")
			}
			events = append(events, event)
		case <-deadline:
			t.Fatalf("timed out waiting for events; got %#v", events)
		}
	}
	if events[0].Event != "chunk" || events[1].Event != "end" {
		t.Fatalf("events = %#v", events)
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

func TestSQLiteRuntimeStatePlaneSharesOneDatabase(t *testing.T) {
	path := t.TempDir() + "/runtime-state.sqlite3"
	plane, err := newSQLiteRuntimeStatePlane(path)
	if err != nil {
		t.Fatalf("newSQLiteRuntimeStatePlane() error = %v", err)
	}
	defer func() { _ = closeRuntimeStatePlane(plane) }()

	plane.Snapshots.SaveRunSnapshot(RunSnapshot{
		Record: RunRecord{RunID: "run-1", ThreadID: "thread-1", Status: "running"},
	})
	plane.Events.AppendRunEvent("run-1", RunEvent{ID: "run-1:1", Event: "updates", RunID: "run-1", ThreadID: "thread-1"})
	plane.Threads.MarkThreadStatus("thread-1", "busy")

	snapshot, ok := plane.Snapshots.LoadRunSnapshot("run-1")
	if !ok || snapshot.Record.Status != "running" {
		t.Fatalf("snapshot = %#v ok=%v", snapshot, ok)
	}
	events := plane.Events.LoadRunEvents("run-1")
	if len(events) != 1 || events[0].Event != "updates" {
		t.Fatalf("events = %#v", events)
	}
	thread, ok := plane.Threads.LoadThreadRuntimeState("thread-1")
	if !ok || thread.Status != "busy" {
		t.Fatalf("thread = %#v ok=%v", thread, ok)
	}
}
