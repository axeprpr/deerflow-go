package harnessruntime

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type workerRunStateRuntime struct {
	snapshots RunSnapshotStore
	threads   ThreadStateStore
}

type workerCompletionRuntime struct {
	threads ThreadStateStore
}

func (r workerRunStateRuntime) SaveRunRecord(record RunRecord) {
	NewSnapshotStoreService(r.snapshots).SaveRecord(record)
}

func (r workerRunStateRuntime) LoadThreadTaskState(threadID string) harness.TaskState {
	if r.threads != nil {
		if state, ok := r.threads.LoadThreadRuntimeState(threadID); ok {
			if taskState, ok := harness.ParseTaskState(state.Metadata[DefaultTaskStateMetadataKey]); ok {
				return taskState
			}
		}
	}
	return harness.TaskState{}
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

func (r workerCompletionRuntime) SetThreadTitle(threadID string, title string) {
	if r.threads != nil {
		r.threads.SetThreadMetadata(threadID, "title", title)
	}
}

func (r workerCompletionRuntime) LoadThreadTaskState(threadID string) harness.TaskState {
	if r.threads != nil {
		if state, ok := r.threads.LoadThreadRuntimeState(threadID); ok {
			if taskState, ok := harness.ParseTaskState(state.Metadata[DefaultTaskStateMetadataKey]); ok {
				return taskState
			}
		}
	}
	return harness.TaskState{}
}

func (r workerCompletionRuntime) SetThreadTaskState(threadID string, taskState harness.TaskState) {
	if r.threads != nil {
		r.threads.SetThreadMetadata(threadID, DefaultTaskStateMetadataKey, taskState.Value())
	}
}

func (r workerCompletionRuntime) ClearThreadTaskState(threadID string) {
	if r.threads != nil {
		r.threads.ClearThreadMetadata(threadID, DefaultTaskStateMetadataKey)
	}
}

func (r workerCompletionRuntime) SetThreadTaskLifecycle(threadID string, lifecycle TaskLifecycleDescriptor) {
	if r.threads != nil {
		r.threads.SetThreadMetadata(threadID, DefaultTaskLifecycleMetadataKey, lifecycle.Value())
	}
}

func (r workerCompletionRuntime) ClearThreadTaskLifecycle(threadID string) {
	if r.threads != nil {
		r.threads.ClearThreadMetadata(threadID, DefaultTaskLifecycleMetadataKey)
	}
}

func (r workerCompletionRuntime) SetThreadInterrupts(threadID string, interrupts []any) {
	if r.threads != nil {
		r.threads.SetThreadMetadata(threadID, "interrupts", interrupts)
	}
}

func (r workerCompletionRuntime) ClearThreadInterrupts(threadID string) {
	if r.threads != nil {
		r.threads.ClearThreadMetadata(threadID, "interrupts")
	}
}

func (r workerCompletionRuntime) MarkThreadStatus(threadID string, status string) {
	if r.threads != nil {
		r.threads.MarkThreadStatus(threadID, status)
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
		CreatedAt: now,
		UpdatedAt: now,
	}
	record.Outcome = NewOutcomeService().DescribeRunning(record)
	return record
}
