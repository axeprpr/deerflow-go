package harnessruntime

type EventFeedRuntime interface {
	LoadRunEvents(runID string) []RunEvent
	SubscribeRunEvents(runID string, buffer int) (<-chan RunEvent, func())
}

type EventFeedService struct {
	runtime EventFeedRuntime
}

func NewEventFeedService(runtime EventFeedRuntime) EventFeedService {
	return EventFeedService{runtime: runtime}
}

func (s EventFeedService) Replay(runID string) ([]RunEvent, bool) {
	if s.runtime == nil {
		return nil, false
	}
	events := append([]RunEvent(nil), s.runtime.LoadRunEvents(runID)...)
	replayedEnd := false
	for _, event := range events {
		if event.Event == "end" {
			replayedEnd = true
		}
	}
	return events, replayedEnd
}

func (s EventFeedService) Subscribe(runID string, buffer int) (<-chan RunEvent, func()) {
	if s.runtime == nil {
		ch := make(chan RunEvent)
		close(ch)
		return ch, func() {}
	}
	return s.runtime.SubscribeRunEvents(runID, buffer)
}
