package harnessruntime

import "testing"

func TestInMemoryRunEventStoreReplaceAndSubscribe(t *testing.T) {
	store := NewInMemoryRunEventStore()
	store.ReplaceRunEvents("run-1", []RunEvent{{ID: "run-1:1", Event: "metadata", RunID: "run-1"}})
	if got := store.NextRunEventIndex("run-1"); got != 2 {
		t.Fatalf("NextRunEventIndex() = %d", got)
	}
	ch, unsubscribe := store.SubscribeRunEvents("run-1", 1)
	defer unsubscribe()
	store.AppendRunEvent("run-1", RunEvent{ID: "run-1:2", Event: "end", RunID: "run-1"})
	event := <-ch
	if event.Event != "end" {
		t.Fatalf("event = %#v", event)
	}
}

func TestJSONFileRunEventStorePersistsEvents(t *testing.T) {
	store := NewJSONFileRunEventStore(t.TempDir())
	store.ReplaceRunEvents("run-1", []RunEvent{
		{ID: "run-1:1", Event: "metadata", RunID: "run-1"},
		{ID: "run-1:2", Event: "end", RunID: "run-1"},
	})
	events := store.LoadRunEvents("run-1")
	if len(events) != 2 || events[1].Event != "end" {
		t.Fatalf("events = %#v", events)
	}
}
