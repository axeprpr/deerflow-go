package harnessruntime

import "fmt"

type RunEventContext struct {
	Attempt         int
	ResumeFromEvent int
	ResumeReason    string
}

type RunEvent struct {
	ID              string
	Event           string
	Data            any
	RunID           string
	ThreadID        string
	Attempt         int
	ResumeFromEvent int
	ResumeReason    string
}

type EventLogService struct {
	store RunEventRecorder
}

func NewEventLogService(store RunEventRecorder) EventLogService {
	return EventLogService{store: store}
}

func (s EventLogService) Record(runID string, threadID string, eventType string, data any) RunEvent {
	return s.RecordWithContext(RunEventContext{}, runID, threadID, eventType, data)
}

func (s EventLogService) RecordWithContext(ctx RunEventContext, runID string, threadID string, eventType string, data any) RunEvent {
	event := RunEvent{
		Event:           eventType,
		Data:            data,
		RunID:           runID,
		ThreadID:        threadID,
		Attempt:         ctx.Attempt,
		ResumeFromEvent: ctx.ResumeFromEvent,
		ResumeReason:    ctx.ResumeReason,
	}
	if s.store == nil {
		event.ID = fmt.Sprintf("%s:%d", runID, 1)
		return event
	}
	event.ID = fmt.Sprintf("%s:%d", runID, s.store.NextRunEventIndex(runID))
	s.store.AppendRunEvent(runID, event)
	return event
}
