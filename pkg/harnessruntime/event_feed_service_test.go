package harnessruntime

import "testing"

type fakeEventFeedRuntime struct {
	events []RunEvent
	ch     chan RunEvent
}

func (r fakeEventFeedRuntime) LoadRunEvents(_ string) []RunEvent {
	return append([]RunEvent(nil), r.events...)
}

func (r fakeEventFeedRuntime) SubscribeRunEvents(_ string, _ int) (<-chan RunEvent, func()) {
	if r.ch == nil {
		ch := make(chan RunEvent)
		close(ch)
		return ch, func() {}
	}
	return r.ch, func() {}
}

func TestEventFeedServiceReplayDetectsEnd(t *testing.T) {
	service := NewEventFeedService(fakeEventFeedRuntime{
		events: []RunEvent{
			{ID: "1", Event: "metadata"},
			{ID: "2", Event: "end"},
		},
	})

	events, replayedEnd := service.Replay("run-1")
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if !replayedEnd {
		t.Fatal("replayedEnd = false, want true")
	}
}

func TestEventFeedServiceSubscribe(t *testing.T) {
	ch := make(chan RunEvent, 1)
	ch <- RunEvent{ID: "1", Event: "values"}
	close(ch)

	service := NewEventFeedService(fakeEventFeedRuntime{ch: ch})
	sub, unsubscribe := service.Subscribe("run-1", 4)
	defer unsubscribe()

	event, ok := <-sub
	if !ok {
		t.Fatal("subscription closed before event")
	}
	if event.Event != "values" {
		t.Fatalf("event.Event = %q, want values", event.Event)
	}
}
