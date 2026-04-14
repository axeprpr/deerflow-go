package harnessruntime

import (
	"strconv"
	"strings"
	"sync"
)

type EventFeedService struct {
	stream RunEventReplayFeed
}

func NewEventFeedService(stream RunEventReplayFeed) EventFeedService {
	return EventFeedService{stream: stream}
}

func (s EventFeedService) Replay(runID string) ([]RunEvent, bool) {
	return s.ReplayFrom(runID, 0)
}

func (s EventFeedService) ReplayFrom(runID string, afterEventIndex int) ([]RunEvent, bool) {
	if s.stream == nil {
		return nil, false
	}
	events := append([]RunEvent(nil), s.stream.LoadRunEvents(runID)...)
	if afterEventIndex > 0 {
		filtered := make([]RunEvent, 0, len(events))
		for _, event := range events {
			if runEventIndex(event) <= afterEventIndex {
				continue
			}
			filtered = append(filtered, event)
		}
		events = filtered
	}
	for i := range events {
		events[i] = normalizeRunEventForFeed(events[i])
	}
	replayedEnd := false
	for _, event := range events {
		if event.Event == "end" {
			replayedEnd = true
		}
	}
	return events, replayedEnd
}

func (s EventFeedService) Subscribe(runID string, buffer int) (<-chan RunEvent, func()) {
	if s.stream == nil {
		ch := make(chan RunEvent)
		close(ch)
		return ch, func() {}
	}
	source, unsubscribeSource := s.stream.SubscribeRunEvents(runID, buffer)
	out := make(chan RunEvent, buffer)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case event, ok := <-source:
				if !ok {
					return
				}
				event = normalizeRunEventForFeed(event)
				select {
				case out <- event:
				case <-done:
					return
				}
			}
		}
	}()
	return out, func() {
		once.Do(func() {
			close(done)
			unsubscribeSource()
		})
	}
}

func runEventIndex(event RunEvent) int {
	if event.ID != "" {
		if idx := strings.LastIndex(event.ID, ":"); idx >= 0 && idx+1 < len(event.ID) {
			if n, err := strconv.Atoi(strings.TrimSpace(event.ID[idx+1:])); err == nil && n > 0 {
				return n
			}
		}
		if n, err := strconv.Atoi(strings.TrimSpace(event.ID)); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func normalizeRunEventForFeed(event RunEvent) RunEvent {
	descriptor := event.Outcome
	if len(descriptor.PendingTasks) == 0 && len(descriptor.TaskLifecycle.PendingTasks) > 0 {
		descriptor.PendingTasks = append([]string(nil), descriptor.TaskLifecycle.PendingTasks...)
	}
	if len(descriptor.ExpectedArtifacts) == 0 && len(descriptor.TaskLifecycle.ExpectedArtifacts) > 0 {
		descriptor.ExpectedArtifacts = append([]string(nil), descriptor.TaskLifecycle.ExpectedArtifacts...)
	}
	if descriptor.Attempt <= 0 && event.Attempt > 0 {
		descriptor.Attempt = event.Attempt
	}
	if descriptor.ResumeFromEvent <= 0 && event.ResumeFromEvent > 0 {
		descriptor.ResumeFromEvent = event.ResumeFromEvent
	}
	if strings.TrimSpace(descriptor.ResumeReason) == "" && strings.TrimSpace(event.ResumeReason) != "" {
		descriptor.ResumeReason = strings.TrimSpace(event.ResumeReason)
	}
	if event.Attempt <= 0 && descriptor.Attempt > 0 {
		event.Attempt = descriptor.Attempt
	}
	if event.ResumeFromEvent <= 0 && descriptor.ResumeFromEvent > 0 {
		event.ResumeFromEvent = descriptor.ResumeFromEvent
	}
	if strings.TrimSpace(event.ResumeReason) == "" && strings.TrimSpace(descriptor.ResumeReason) != "" {
		event.ResumeReason = descriptor.ResumeReason
	}
	event.Outcome = descriptor
	return event
}
