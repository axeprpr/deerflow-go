package langgraphcompat

import (
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestRunMapperRoundTrip(t *testing.T) {
	now := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	record := harnessruntime.RunRecord{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	run := runFromRecord(record)
	roundTrip := runRecordFromRun(run)
	if roundTrip != record {
		t.Fatalf("roundTrip = %+v want %+v", roundTrip, record)
	}
}

func TestApplyRunRecordMutatesCompatRun(t *testing.T) {
	run := &Run{RunID: "old", ThreadID: "thread-old", Status: "queued"}
	record := harnessruntime.RunRecord{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "lead_agent",
		Status:      "success",
	}

	applyRunRecord(run, record)
	if run.RunID != "run-1" || run.ThreadID != "thread-1" || run.Status != "success" {
		t.Fatalf("run = %+v", run)
	}
}
