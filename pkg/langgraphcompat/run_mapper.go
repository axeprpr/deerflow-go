package langgraphcompat

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func runFromRecord(record harnessruntime.RunRecord) *Run {
	record.Outcome = harnessruntime.NewOutcomeService().BindRecord(record, record.Outcome)
	return &Run{
		RunID:           record.RunID,
		ThreadID:        record.ThreadID,
		AssistantID:     record.AssistantID,
		Attempt:         record.Attempt,
		ResumeFromEvent: record.ResumeFromEvent,
		ResumeReason:    record.ResumeReason,
		Status:          record.Status,
		Error:           record.Error,
		Outcome:         record.Outcome,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
	}
}

func runFromSnapshot(snapshot harnessruntime.RunSnapshot) *Run {
	run := runFromRecord(snapshot.Record)
	record := runRecordFromRun(run)
	run.Events = make([]StreamEvent, 0, len(snapshot.Events))
	for _, event := range snapshot.Events {
		run.Events = append(run.Events, streamEventFromRuntimeEvent(normalizeRuntimeEventForRun(event, record)))
	}
	return run
}

func runRecordFromRun(run *Run) harnessruntime.RunRecord {
	if run == nil {
		return harnessruntime.RunRecord{}
	}
	return harnessruntime.RunRecord{
		RunID:           run.RunID,
		ThreadID:        run.ThreadID,
		AssistantID:     run.AssistantID,
		Attempt:         run.Attempt,
		ResumeFromEvent: run.ResumeFromEvent,
		ResumeReason:    run.ResumeReason,
		Status:          run.Status,
		Error:           run.Error,
		Outcome:         run.Outcome,
		CreatedAt:       run.CreatedAt,
		UpdatedAt:       run.UpdatedAt,
	}
}

func runSnapshotFromRun(run *Run) harnessruntime.RunSnapshot {
	if run == nil {
		return harnessruntime.RunSnapshot{}
	}
	snapshot := harnessruntime.RunSnapshot{
		Record: runRecordFromRun(run),
		Events: make([]harnessruntime.RunEvent, 0, len(run.Events)),
	}
	for _, event := range run.Events {
		snapshot.Events = append(snapshot.Events, runtimeEventFromStreamEvent(event))
	}
	return snapshot
}

func applyRunRecord(run *Run, record harnessruntime.RunRecord) {
	if run == nil {
		return
	}
	run.RunID = record.RunID
	run.ThreadID = record.ThreadID
	run.AssistantID = record.AssistantID
	run.Attempt = record.Attempt
	run.ResumeFromEvent = record.ResumeFromEvent
	run.ResumeReason = record.ResumeReason
	run.Status = record.Status
	run.Error = record.Error
	run.Outcome = record.Outcome
	run.CreatedAt = record.CreatedAt
	run.UpdatedAt = record.UpdatedAt
}

func streamEventFromRuntimeEvent(event harnessruntime.RunEvent) StreamEvent {
	return StreamEvent{
		ID:              event.ID,
		Event:           event.Event,
		Data:            event.Data,
		RunID:           event.RunID,
		ThreadID:        event.ThreadID,
		Attempt:         event.Attempt,
		ResumeFromEvent: event.ResumeFromEvent,
		ResumeReason:    event.ResumeReason,
		Outcome:         event.Outcome,
	}
}

func runtimeEventFromStreamEvent(event StreamEvent) harnessruntime.RunEvent {
	return harnessruntime.RunEvent{
		ID:              event.ID,
		Event:           event.Event,
		Data:            event.Data,
		RunID:           event.RunID,
		ThreadID:        event.ThreadID,
		Attempt:         event.Attempt,
		ResumeFromEvent: event.ResumeFromEvent,
		ResumeReason:    event.ResumeReason,
		Outcome:         event.Outcome,
	}
}

func normalizeRuntimeEventForRun(event harnessruntime.RunEvent, record harnessruntime.RunRecord) harnessruntime.RunEvent {
	outcome := event.Outcome
	service := harnessruntime.NewOutcomeService()
	if strings.TrimSpace(record.RunID) != "" {
		outcome = service.BindRecord(record, outcome)
		if strings.TrimSpace(outcome.RunStatus) == "" {
			outcome = service.BindRecord(record, record.Outcome)
		}
	}
	if len(outcome.PendingTasks) == 0 && len(outcome.TaskLifecycle.PendingTasks) > 0 {
		outcome.PendingTasks = append([]string(nil), outcome.TaskLifecycle.PendingTasks...)
	}
	if len(outcome.ExpectedArtifacts) == 0 && len(outcome.TaskLifecycle.ExpectedArtifacts) > 0 {
		outcome.ExpectedArtifacts = append([]string(nil), outcome.TaskLifecycle.ExpectedArtifacts...)
	}
	if outcome.Attempt <= 0 && event.Attempt > 0 {
		outcome.Attempt = event.Attempt
	}
	if outcome.ResumeFromEvent <= 0 && event.ResumeFromEvent > 0 {
		outcome.ResumeFromEvent = event.ResumeFromEvent
	}
	if strings.TrimSpace(outcome.ResumeReason) == "" && strings.TrimSpace(event.ResumeReason) != "" {
		outcome.ResumeReason = strings.TrimSpace(event.ResumeReason)
	}
	event.Outcome = outcome
	if event.Attempt <= 0 && outcome.Attempt > 0 {
		event.Attempt = outcome.Attempt
	}
	if event.ResumeFromEvent <= 0 && outcome.ResumeFromEvent > 0 {
		event.ResumeFromEvent = outcome.ResumeFromEvent
	}
	if strings.TrimSpace(event.ResumeReason) == "" && strings.TrimSpace(outcome.ResumeReason) != "" {
		event.ResumeReason = outcome.ResumeReason
	}
	return event
}
