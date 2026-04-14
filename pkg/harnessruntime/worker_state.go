package harnessruntime

import "time"

type workerRunStateRuntime struct {
	snapshots RunSnapshotStore
	threads   ThreadStateStore
}

func (r workerRunStateRuntime) SaveRunRecord(record RunRecord) {
	NewSnapshotStoreService(r.snapshots).SaveRecord(record)
}

func (r workerRunStateRuntime) MarkThreadStatus(threadID string, status string) {
	if r.threads != nil {
		r.threads.MarkThreadStatus(threadID, status)
	}
}

func (r workerRunStateRuntime) SetThreadTaskLifecycle(threadID string, lifecycle TaskLifecycleDescriptor) {
	if r.threads != nil {
		r.threads.SetThreadMetadata(threadID, DefaultTaskLifecycleMetadataKey, lifecycle.Value())
	}
}

func (r workerRunStateRuntime) ClearThreadTaskLifecycle(threadID string) {
	if r.threads != nil {
		r.threads.ClearThreadMetadata(threadID, DefaultTaskLifecycleMetadataKey)
	}
}

func loadWorkerRunRecord(plan WorkerExecutionPlan, snapshots RunSnapshotStore) RunRecord {
	record, ok := NewSnapshotStoreService(snapshots).LoadRecord(plan.RunID)
	if ok {
		return record
	}
	now := plan.SubmittedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	record = RunRecord{
		RunID:           plan.RunID,
		ThreadID:        plan.ThreadID,
		AssistantID:     plan.AssistantID,
		Attempt:         plan.Attempt,
		ResumeFromEvent: plan.ResumeFromEvent,
		ResumeReason:    plan.ResumeReason,
		Status:          "running",
		Outcome: RunOutcomeDescriptor{
			RunStatus:       "running",
			Attempt:         plan.Attempt,
			ResumeFromEvent: plan.ResumeFromEvent,
			ResumeReason:    plan.ResumeReason,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	return record
}
