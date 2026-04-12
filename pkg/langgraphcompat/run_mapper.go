package langgraphcompat

import "github.com/axeprpr/deerflow-go/pkg/harnessruntime"

func runFromRecord(record harnessruntime.RunRecord) *Run {
	return &Run{
		RunID:           record.RunID,
		ThreadID:        record.ThreadID,
		AssistantID:     record.AssistantID,
		Attempt:         record.Attempt,
		ResumeFromEvent: record.ResumeFromEvent,
		ResumeReason:    record.ResumeReason,
		Status:          record.Status,
		Error:           record.Error,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
	}
}

func runFromSnapshot(snapshot harnessruntime.RunSnapshot) *Run {
	run := runFromRecord(snapshot.Record)
	run.Events = make([]StreamEvent, 0, len(snapshot.Events))
	for _, event := range snapshot.Events {
		run.Events = append(run.Events, streamEventFromRuntimeEvent(event))
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
	run.CreatedAt = record.CreatedAt
	run.UpdatedAt = record.UpdatedAt
}

func streamEventFromRuntimeEvent(event harnessruntime.RunEvent) StreamEvent {
	return StreamEvent{
		ID:       event.ID,
		Event:    event.Event,
		Data:     event.Data,
		RunID:    event.RunID,
		ThreadID: event.ThreadID,
	}
}

func runtimeEventFromStreamEvent(event StreamEvent) harnessruntime.RunEvent {
	return harnessruntime.RunEvent{
		ID:       event.ID,
		Event:    event.Event,
		Data:     event.Data,
		RunID:    event.RunID,
		ThreadID: event.ThreadID,
	}
}
