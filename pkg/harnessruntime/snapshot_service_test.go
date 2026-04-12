package harnessruntime

import "testing"

type fakeSnapshotRuntime struct {
	snapshots map[string]RunSnapshot
}

func (r *fakeSnapshotRuntime) LoadRunSnapshot(runID string) (RunSnapshot, bool) {
	snapshot, ok := r.snapshots[runID]
	return snapshot, ok
}

func (r *fakeSnapshotRuntime) ListRunSnapshots(threadID string) []RunSnapshot {
	out := make([]RunSnapshot, 0)
	for _, snapshot := range r.snapshots {
		if threadID != "" && snapshot.Record.ThreadID != threadID {
			continue
		}
		out = append(out, snapshot)
	}
	return out
}

func (r *fakeSnapshotRuntime) SaveRunSnapshot(snapshot RunSnapshot) {
	if r.snapshots == nil {
		r.snapshots = map[string]RunSnapshot{}
	}
	r.snapshots[snapshot.Record.RunID] = snapshot
}

func TestSnapshotStoreServicePreservesEventsWhenSavingRecord(t *testing.T) {
	runtime := &fakeSnapshotRuntime{
		snapshots: map[string]RunSnapshot{
			"run-1": {
				Record: RunRecord{RunID: "run-1", Status: "running"},
				Events: []RunEvent{{ID: "run-1:1", Event: "metadata"}},
			},
		},
	}
	service := NewSnapshotStoreService(runtime)
	service.SaveRecord(RunRecord{RunID: "run-1", Status: "success"})

	snapshot, ok := runtime.LoadRunSnapshot("run-1")
	if !ok {
		t.Fatal("snapshot missing")
	}
	if snapshot.Record.Status != "success" {
		t.Fatalf("status = %q, want success", snapshot.Record.Status)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].Event != "metadata" {
		t.Fatalf("events = %#v", snapshot.Events)
	}
}

func TestSnapshotStoreServiceAppendsEvents(t *testing.T) {
	runtime := &fakeSnapshotRuntime{}
	service := NewSnapshotStoreService(runtime)
	service.SaveRecord(RunRecord{RunID: "run-1", ThreadID: "thread-1"})
	service.AppendEvent("run-1", RunEvent{ID: "run-1:1", Event: "metadata"})

	events := service.LoadEvents("run-1")
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if got := service.NextEventIndex("run-1"); got != 2 {
		t.Fatalf("NextEventIndex = %d, want 2", got)
	}
}

func TestSnapshotStoreServicePreservesRecoveryMetadata(t *testing.T) {
	runtime := &fakeSnapshotRuntime{}
	service := NewSnapshotStoreService(runtime)
	service.SaveRecord(RunRecord{
		RunID:           "run-1",
		ThreadID:        "thread-1",
		Attempt:         3,
		ResumeFromEvent: 9,
		ResumeReason:    "resume-after-crash",
	})

	record, ok := service.LoadRecord("run-1")
	if !ok {
		t.Fatal("LoadRecord() missing saved record")
	}
	if record.Attempt != 3 {
		t.Fatalf("Attempt = %d, want 3", record.Attempt)
	}
	if record.ResumeFromEvent != 9 {
		t.Fatalf("ResumeFromEvent = %d, want 9", record.ResumeFromEvent)
	}
	if record.ResumeReason != "resume-after-crash" {
		t.Fatalf("ResumeReason = %q", record.ResumeReason)
	}
}
