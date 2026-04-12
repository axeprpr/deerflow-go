package harnessruntime

import "fmt"

type RunEvent struct {
	ID       string
	Event    string
	Data     any
	RunID    string
	ThreadID string
}

type EventLogRuntime interface {
	NextRunEventIndex(runID string) int
	AppendRunEvent(runID string, event RunEvent)
}

type EventLogService struct {
	runtime EventLogRuntime
}

func NewEventLogService(runtime EventLogRuntime) EventLogService {
	return EventLogService{runtime: runtime}
}

func (s EventLogService) Record(runID string, threadID string, eventType string, data any) RunEvent {
	event := RunEvent{
		Event:    eventType,
		Data:     data,
		RunID:    runID,
		ThreadID: threadID,
	}
	if s.runtime == nil {
		event.ID = fmt.Sprintf("%s:%d", runID, 1)
		return event
	}
	event.ID = fmt.Sprintf("%s:%d", runID, s.runtime.NextRunEventIndex(runID))
	s.runtime.AppendRunEvent(runID, event)
	return event
}
