package harnessruntime

import "testing"

func TestInMemoryRunStoreSaveListAndSubscribe(t *testing.T) {
	store := NewInMemoryRunStore()
	store.SaveRunSnapshot(RunSnapshot{
		Record: RunRecord{RunID: "run-1", ThreadID: "thread-1", Status: "running"},
	})

	snapshots := store.ListRunSnapshots("thread-1")
	if len(snapshots) != 1 || snapshots[0].Record.RunID != "run-1" {
		t.Fatalf("ListRunSnapshots() = %#v", snapshots)
	}

	ch, unsubscribe := store.SubscribeRunEvents("run-1", 1)
	defer unsubscribe()
	store.AppendRunEvent("run-1", RunEvent{ID: "run-1:1", Event: "metadata", RunID: "run-1", ThreadID: "thread-1"})

	event := <-ch
	if event.Event != "metadata" {
		t.Fatalf("event.Event = %q, want metadata", event.Event)
	}
	if got := store.NextRunEventIndex("run-1"); got != 2 {
		t.Fatalf("NextRunEventIndex() = %d, want 2", got)
	}
}

