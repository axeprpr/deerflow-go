package harnessruntime

import "testing"

func TestJSONFileRunStorePersistsSnapshots(t *testing.T) {
	store := NewJSONFileRunStore(t.TempDir())
	store.SaveRunSnapshot(RunSnapshot{
		Record: RunRecord{RunID: "run-1", ThreadID: "thread-1", Status: "success"},
		Events: []RunEvent{{ID: "run-1:1", Event: "end", RunID: "run-1", ThreadID: "thread-1"}},
	})

	loaded, ok := store.LoadRunSnapshot("run-1")
	if !ok {
		t.Fatal("LoadRunSnapshot() = false, want true")
	}
	if loaded.Record.Status != "success" {
		t.Fatalf("loaded status = %q, want success", loaded.Record.Status)
	}
	if len(loaded.Events) != 1 || loaded.Events[0].Event != "end" {
		t.Fatalf("loaded events = %#v", loaded.Events)
	}
}

func TestJSONFileRunStoreListsByThread(t *testing.T) {
	store := NewJSONFileRunStore(t.TempDir())
	store.SaveRunSnapshot(RunSnapshot{Record: RunRecord{RunID: "run-1", ThreadID: "thread-a"}})
	store.SaveRunSnapshot(RunSnapshot{Record: RunRecord{RunID: "run-2", ThreadID: "thread-b"}})

	list := store.ListRunSnapshots("thread-a")
	if len(list) != 1 || list[0].Record.RunID != "run-1" {
		t.Fatalf("ListRunSnapshots() = %#v", list)
	}
}

