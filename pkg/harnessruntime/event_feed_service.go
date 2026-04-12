package harnessruntime

import (
	"strconv"
	"strings"
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
	return s.stream.SubscribeRunEvents(runID, buffer)
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
