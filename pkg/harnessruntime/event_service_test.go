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

func TestEventLogServiceRecordWithContextPreservesRecoveryMetadata(t *testing.T) {
	runtime := &fakeEventLogRuntime{nextIndex: 2}
	service := NewEventLogService(runtime)

	event := service.RecordWithContext(RunEventContext{
		Attempt:         3,
		ResumeFromEvent: 7,
		ResumeReason:    "replay",
	}, "run-1", "thread-1", "updates", map[string]any{"ok": true})

	if event.Attempt != 3 {
		t.Fatalf("event.Attempt = %d, want 3", event.Attempt)
	}
	if event.ResumeFromEvent != 7 {
		t.Fatalf("event.ResumeFromEvent = %d, want 7", event.ResumeFromEvent)
	}
	if event.ResumeReason != "replay" {
		t.Fatalf("event.ResumeReason = %q, want replay", event.ResumeReason)
	}
	if len(runtime.events) != 1 {
		t.Fatalf("events = %d, want 1", len(runtime.events))
	}
	if runtime.events[0].Attempt != 3 || runtime.events[0].ResumeFromEvent != 7 || runtime.events[0].ResumeReason != "replay" {
		t.Fatalf("saved event = %#v", runtime.events[0])
	}
}
