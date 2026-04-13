package harnessruntime

import "testing"

func TestLoadWorkerRunRecordFallsBackToPlanMetadata(t *testing.T) {
	plan := WorkerExecutionPlan{
		RunID:           "run-worker-record",
		ThreadID:        "thread-worker-record",
		AssistantID:     "assistant-1",
		Attempt:         2,
		ResumeFromEvent: 4,
		ResumeReason:    "retry",
	}

	record := loadWorkerRunRecord(plan, nil)
	if record.RunID != plan.RunID || record.ThreadID != plan.ThreadID || record.AssistantID != plan.AssistantID {
		t.Fatalf("record = %+v", record)
	}
	if record.Attempt != 2 || record.ResumeFromEvent != 4 || record.ResumeReason != "retry" {
		t.Fatalf("record = %+v", record)
	}
	if record.Status != "running" || record.Outcome.RunStatus != "running" {
		t.Fatalf("record = %+v", record)
	}
}

func TestWorkerRunStateRuntimePersistsRunRecordAndThreadStatus(t *testing.T) {
	snapshots := NewInMemoryRunStore()
	threads := NewInMemoryThreadStateStore()
	runtime := workerRunStateRuntime{
		snapshots: snapshots,
		threads:   threads,
	}

	record := RunRecord{
		RunID:       "run-worker-state",
		ThreadID:    "thread-worker-state",
		AssistantID: "assistant-1",
		Status:      "running",
	}
	runtime.SaveRunRecord(record)
	runtime.MarkThreadStatus(record.ThreadID, "busy")

	saved, ok := NewSnapshotStoreService(snapshots).LoadRecord(record.RunID)
	if !ok {
		t.Fatal("LoadRecord() = false, want true")
	}
	if saved.RunID != record.RunID {
		t.Fatalf("saved = %+v", saved)
	}
	state, ok := threads.LoadThreadRuntimeState(record.ThreadID)
	if !ok || state.Status != "busy" {
		t.Fatalf("thread state = %+v, ok=%v", state, ok)
	}
}
