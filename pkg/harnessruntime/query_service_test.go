package harnessruntime

import (
	"testing"
	"time"
)

type fakeQueryRuntime struct {
	record    RunRecord
	recordOK  bool
	records   []RunRecord
	hasThread bool
}

func (r fakeQueryRuntime) LoadRunRecord(_ string) (RunRecord, bool) {
	return r.record, r.recordOK
}

func (r fakeQueryRuntime) ListRunRecords(_ string) []RunRecord {
	return append([]RunRecord(nil), r.records...)
}

func (r fakeQueryRuntime) HasThread(_ string) bool {
	return r.hasThread
}

func TestQueryServiceRunNormalizesOutcomeDescriptor(t *testing.T) {
	service := NewQueryService(fakeQueryRuntime{
		recordOK: true,
		record: RunRecord{
			RunID:           "run-1",
			ThreadID:        "thread-1",
			Attempt:         3,
			ResumeFromEvent: 7,
			ResumeReason:    "replay",
			Outcome: RunOutcomeDescriptor{
				RunStatus: "running",
				TaskLifecycle: TaskLifecycleDescriptor{
					Status:            "running",
					PendingTasks:      []string{"delegate research"},
					ExpectedArtifacts: []string{"/mnt/user-data/outputs/report.md"},
				},
			},
		},
	})

	record, ok := service.Run("thread-1", "run-1")
	if !ok {
		t.Fatal("Run() = false, want true")
	}
	if record.Outcome.Attempt != 3 || record.Outcome.ResumeFromEvent != 7 || record.Outcome.ResumeReason != "replay" {
		t.Fatalf("outcome recovery = %+v", record.Outcome)
	}
	if len(record.Outcome.PendingTasks) != 1 || record.Outcome.PendingTasks[0] != "delegate research" {
		t.Fatalf("pending tasks = %#v", record.Outcome.PendingTasks)
	}
	if len(record.Outcome.ExpectedArtifacts) != 1 || record.Outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("expected artifacts = %#v", record.Outcome.ExpectedArtifacts)
	}
}

func TestQueryServiceListThreadRunsNormalizesAndSorts(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	service := NewQueryService(fakeQueryRuntime{
		records: []RunRecord{
			{
				RunID:           "run-older",
				ThreadID:        "thread-1",
				Attempt:         1,
				ResumeFromEvent: 0,
				ResumeReason:    "",
				CreatedAt:       now.Add(-time.Minute),
				Outcome: RunOutcomeDescriptor{
					RunStatus: "running",
					TaskLifecycle: TaskLifecycleDescriptor{
						Status:       "running",
						PendingTasks: []string{"first task"},
					},
				},
			},
			{
				RunID:           "run-newer",
				ThreadID:        "thread-1",
				Attempt:         2,
				ResumeFromEvent: 5,
				ResumeReason:    "replay",
				CreatedAt:       now,
				Outcome: RunOutcomeDescriptor{
					RunStatus: "running",
					TaskLifecycle: TaskLifecycleDescriptor{
						Status:            "running",
						ExpectedArtifacts: []string{"/mnt/user-data/outputs/report.md"},
					},
				},
			},
		},
	})

	records := service.ListThreadRuns("thread-1")
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].RunID != "run-newer" || records[1].RunID != "run-older" {
		t.Fatalf("records order = [%s, %s]", records[0].RunID, records[1].RunID)
	}
	if records[0].Outcome.Attempt != 2 || records[0].Outcome.ResumeFromEvent != 5 || records[0].Outcome.ResumeReason != "replay" {
		t.Fatalf("newer outcome recovery = %+v", records[0].Outcome)
	}
	if len(records[0].Outcome.ExpectedArtifacts) != 1 || records[0].Outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("newer expected artifacts = %#v", records[0].Outcome.ExpectedArtifacts)
	}
	if len(records[1].Outcome.PendingTasks) != 1 || records[1].Outcome.PendingTasks[0] != "first task" {
		t.Fatalf("older pending tasks = %#v", records[1].Outcome.PendingTasks)
	}
}
