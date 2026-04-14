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

func TestEventFeedServiceReplayFromSkipsAlreadyReplayedEvents(t *testing.T) {
	service := NewEventFeedService(fakeEventFeedRuntime{
		events: []RunEvent{
			{ID: "run-1:1", Event: "metadata"},
			{ID: "run-1:2", Event: "values"},
			{ID: "run-1:3", Event: "end"},
		},
	})

	events, replayedEnd := service.ReplayFrom("run-1", 1)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].ID != "run-1:2" || events[1].ID != "run-1:3" {
		t.Fatalf("events = %#v", events)
	}
	if !replayedEnd {
		t.Fatal("replayedEnd = false, want true")
	}
}

func TestEventFeedServiceReplayNormalizesOutcomeTaskFields(t *testing.T) {
	service := NewEventFeedService(fakeEventFeedRuntime{
		events: []RunEvent{
			{
				ID:      "run-1:2",
				Event:   "task_running",
				Attempt: 3,
				Outcome: RunOutcomeDescriptor{
					RunStatus: "running",
					TaskLifecycle: TaskLifecycleDescriptor{
						Status:            "running",
						PendingTasks:      []string{"delegate research"},
						ExpectedArtifacts: []string{"/mnt/user-data/outputs/report.md"},
					},
				},
			},
		},
	})

	events, replayedEnd := service.Replay("run-1")
	if replayedEnd {
		t.Fatal("replayedEnd = true, want false")
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Outcome.Attempt != 3 {
		t.Fatalf("outcome attempt = %d, want 3", events[0].Outcome.Attempt)
	}
	if len(events[0].Outcome.PendingTasks) != 1 || events[0].Outcome.PendingTasks[0] != "delegate research" {
		t.Fatalf("pending tasks = %#v", events[0].Outcome.PendingTasks)
	}
	if len(events[0].Outcome.ExpectedArtifacts) != 1 || events[0].Outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("expected artifacts = %#v", events[0].Outcome.ExpectedArtifacts)
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

func TestEventFeedServiceSubscribeNormalizesOutcomeAttemptAndResume(t *testing.T) {
	ch := make(chan RunEvent, 1)
	ch <- RunEvent{
		ID:              "run-1:4",
		Event:           "values",
		Attempt:         2,
		ResumeFromEvent: 3,
		ResumeReason:    "replay",
		Outcome:         RunOutcomeDescriptor{RunStatus: "running"},
	}
	close(ch)

	service := NewEventFeedService(fakeEventFeedRuntime{ch: ch})
	sub, unsubscribe := service.Subscribe("run-1", 4)
	defer unsubscribe()

	event, ok := <-sub
	if !ok {
		t.Fatal("subscription closed before event")
	}
	if event.Outcome.Attempt != 2 || event.Outcome.ResumeFromEvent != 3 || event.Outcome.ResumeReason != "replay" {
		t.Fatalf("event outcome = %+v", event.Outcome)
	}
}
