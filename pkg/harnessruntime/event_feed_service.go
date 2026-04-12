package harnessruntime

type EventFeedService struct {
	stream RunEventReplayFeed
}

func NewEventFeedService(stream RunEventReplayFeed) EventFeedService {
	return EventFeedService{stream: stream}
}

func (s EventFeedService) Replay(runID string) ([]RunEvent, bool) {
	if s.stream == nil {
		return nil, false
	}
	events := append([]RunEvent(nil), s.stream.LoadRunEvents(runID)...)
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
