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
	if strings.TrimSpace(record.Error) != "" {
		out["error"] = record.Error
	}
	return out
}
