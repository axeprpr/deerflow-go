package harnessruntime

import "testing"

type fakeEventLogRuntime struct {
	indexCalls int
	nextIndex  int
	runID      string
	events     []RunEvent
}

func (r *fakeEventLogRuntime) NextRunEventIndex(runID string) int {
	r.indexCalls++
	r.runID = runID
	if r.nextIndex == 0 {
		return 1
	}
	return r.nextIndex
}

func (r *fakeEventLogRuntime) AppendRunEvent(runID string, event RunEvent) {
	r.runID = runID
	r.events = append(r.events, event)
}

func TestEventLogServiceRecordsRuntimeEvent(t *testing.T) {
	runtime := &fakeEventLogRuntime{nextIndex: 3}
	service := NewEventLogService(runtime)

	event := service.Record("run-1", "thread-1", "values", map[string]any{"title": "done"})
	if event.ID != "run-1:3" {
		t.Fatalf("event.ID = %q, want run-1:3", event.ID)
	}
	if event.Event != "values" {
		t.Fatalf("event.Event = %q", event.Event)
	}
	if len(runtime.events) != 1 {
		t.Fatalf("events = %d, want 1", len(runtime.events))
	}
	if runtime.events[0].ID != "run-1:3" {
		t.Fatalf("saved event ID = %q", runtime.events[0].ID)
	}
}

func TestEventLogServiceFallsBackWithoutRuntime(t *testing.T) {
	service := NewEventLogService(nil)
	event := service.Record("run-1", "thread-1", "metadata", nil)
	if event.ID != "run-1:1" {
		t.Fatalf("event.ID = %q, want run-1:1", event.ID)
	}
}
