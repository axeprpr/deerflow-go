package langgraphcompat

import "github.com/axeprpr/deerflow-go/pkg/harnessruntime"

func runFromRecord(record harnessruntime.RunRecord) *Run {
	return &Run{
		RunID:       record.RunID,
		ThreadID:    record.ThreadID,
		AssistantID: record.AssistantID,
		Status:      record.Status,
		Error:       record.Error,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}
}

func runRecordFromRun(run *Run) harnessruntime.RunRecord {
	if run == nil {
		return harnessruntime.RunRecord{}
	}
	return harnessruntime.RunRecord{
		RunID:       run.RunID,
		ThreadID:    run.ThreadID,
		AssistantID: run.AssistantID,
		Status:      run.Status,
		Error:       run.Error,
		CreatedAt:   run.CreatedAt,
		UpdatedAt:   run.UpdatedAt,
	}
}

func applyRunRecord(run *Run, record harnessruntime.RunRecord) {
	if run == nil {
		return
	}
	run.RunID = record.RunID
	run.ThreadID = record.ThreadID
	run.AssistantID = record.AssistantID
	run.Status = record.Status
	run.Error = record.Error
	run.CreatedAt = record.CreatedAt
	run.UpdatedAt = record.UpdatedAt
}
