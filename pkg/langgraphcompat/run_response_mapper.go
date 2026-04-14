package langgraphcompat

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func runResponse(run *Run) map[string]any {
	if run == nil {
		return nil
	}
	return runRecordResponse(runRecordFromRun(run))
}

func runRecordResponse(record harnessruntime.RunRecord) map[string]any {
	out := map[string]any{
		"run_id":       record.RunID,
		"thread_id":    record.ThreadID,
		"assistant_id": record.AssistantID,
		"status":       record.Status,
		"created_at":   record.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":   record.UpdatedAt.Format(time.RFC3339Nano),
	}
	if record.Attempt > 0 {
		out["attempt"] = record.Attempt
	}
	if record.ResumeFromEvent > 0 {
		out["resume_from_event"] = record.ResumeFromEvent
	}
	if strings.TrimSpace(record.ResumeReason) != "" {
		out["resume_reason"] = strings.TrimSpace(record.ResumeReason)
	}
	if strings.TrimSpace(record.Error) != "" {
		out["error"] = record.Error
	}
	outcome := harnessruntime.NewOutcomeService().BindRecord(record, record.Outcome)
	if payload := runOutcomeResponse(outcome); len(payload) > 0 {
		out["outcome"] = payload
	}
	return out
}

func runOutcomeResponse(outcome harnessruntime.RunOutcomeDescriptor) map[string]any {
	payload := map[string]any{}
	if status := strings.TrimSpace(outcome.RunStatus); status != "" {
		payload["run_status"] = status
	}
	if outcome.Interrupted {
		payload["interrupted"] = true
	}
	if errText := strings.TrimSpace(outcome.Error); errText != "" {
		payload["error"] = errText
	}
	if len(outcome.PendingTasks) > 0 {
		payload["pending_tasks"] = append([]string(nil), outcome.PendingTasks...)
	}
	if len(outcome.ExpectedArtifacts) > 0 {
		payload["expected_artifacts"] = append([]string(nil), outcome.ExpectedArtifacts...)
	}
	if !outcome.TaskState.IsZero() {
		payload["task_state"] = outcome.TaskState.Value()
	}
	if !outcome.TaskLifecycle.IsZero() {
		payload["task_lifecycle"] = outcome.TaskLifecycle.Value()
	}
	if outcome.Attempt > 0 {
		payload["attempt"] = outcome.Attempt
	}
	if outcome.ResumeFromEvent > 0 {
		payload["resume_from_event"] = outcome.ResumeFromEvent
	}
	if reason := strings.TrimSpace(outcome.ResumeReason); reason != "" {
		payload["resume_reason"] = reason
	}
	return payload
}
