package harnessruntime

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type RunStateRuntime interface {
	SaveRunRecord(record RunRecord)
	LoadThreadTaskState(threadID string) harness.TaskState
	MarkThreadStatus(threadID string, status string)
	SetThreadTaskLifecycle(threadID string, lifecycle TaskLifecycleDescriptor)
	ClearThreadTaskLifecycle(threadID string)
}

type RunStateService struct {
	runtime RunStateRuntime
	now     func() time.Time
}

type runStateThreadStoreProvider interface {
	ThreadStateStore() ThreadStateStore
}

func NewRunStateService(runtime RunStateRuntime) RunStateService {
	return RunStateService{
		runtime: runtime,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s RunStateService) MarkError(record RunRecord, err error) RunRecord {
	taskState := s.loadTaskState(record.ThreadID)
	outcome := NewOutcomeService().DescribeWithTaskState(record, RunOutcome{RunStatus: "error"}, err.Error(), taskState, false)
	record.Status = outcome.RunStatus
	record.Error = outcome.Error
	record.Outcome = outcome
	record.UpdatedAt = s.now()
	applyThreadMutation := s.shouldMutateThreadState(record)
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		if applyThreadMutation {
			s.runtime.SetThreadTaskLifecycle(record.ThreadID, outcome.TaskLifecycle)
			s.runtime.MarkThreadStatus(record.ThreadID, "error")
		}
	}
	s.clearActiveRunOwnership(record)
	return record
}

func (s RunStateService) Begin(record RunRecord) RunRecord {
	taskState := s.loadTaskState(record.ThreadID)
	if record.Status == "" {
		record.Status = "running"
	}
	record.Outcome = NewOutcomeService().DescribeWithTaskState(record, RunOutcome{RunStatus: "running"}, "", taskState, false)
	record.UpdatedAt = s.now()
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		s.runtime.SetThreadTaskLifecycle(record.ThreadID, record.Outcome.TaskLifecycle)
		s.runtime.MarkThreadStatus(record.ThreadID, "busy")
	}
	s.setActiveRunOwnership(record)
	return record
}

func (s RunStateService) MarkCanceled(record RunRecord) RunRecord {
	taskState := s.loadTaskState(record.ThreadID)
	outcome := NewOutcomeService().DescribeWithTaskState(record, RunOutcome{RunStatus: "interrupted", Interrupted: true}, "", taskState, false)
	record.Status = outcome.RunStatus
	record.Error = outcome.Error
	record.Outcome = outcome
	record.UpdatedAt = s.now()
	applyThreadMutation := s.shouldMutateThreadState(record)
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		if applyThreadMutation {
			s.runtime.SetThreadTaskLifecycle(record.ThreadID, outcome.TaskLifecycle)
			s.runtime.MarkThreadStatus(record.ThreadID, "interrupted")
		}
	}
	s.clearActiveRunOwnership(record)
	return record
}

func (s RunStateService) loadTaskState(threadID string) harness.TaskState {
	if s.runtime == nil {
		return harness.TaskState{}
	}
	taskState, err := harness.NormalizeTaskState(s.runtime.LoadThreadTaskState(threadID))
	if err != nil {
		return harness.TaskState{}
	}
	return taskState
}

func (s RunStateService) Finalize(record RunRecord, outcome CompletionOutcome) RunRecord {
	descriptor := NewOutcomeService().BindRecord(record, outcome.Descriptor)
	record.Status = descriptor.RunStatus
	record.Error = descriptor.Error
	record.Outcome = descriptor
	record.UpdatedAt = s.now()
	applyThreadMutation := s.shouldMutateThreadState(record)
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		if applyThreadMutation {
			if !record.Outcome.TaskLifecycle.IsZero() {
				s.runtime.SetThreadTaskLifecycle(record.ThreadID, record.Outcome.TaskLifecycle)
			} else {
				s.runtime.ClearThreadTaskLifecycle(record.ThreadID)
			}
		}
	}
	s.clearActiveRunOwnership(record)
	return record
}

func (s RunStateService) threadStateStore() ThreadStateStore {
	provider, ok := s.runtime.(runStateThreadStoreProvider)
	if !ok || provider == nil {
		return nil
	}
	return provider.ThreadStateStore()
}

func (s RunStateService) setActiveRunOwnership(record RunRecord) {
	store := s.threadStateStore()
	if store == nil {
		return
	}
	threadID := strings.TrimSpace(record.ThreadID)
	runID := strings.TrimSpace(record.RunID)
	if threadID == "" || runID == "" {
		return
	}
	store.SetThreadMetadata(threadID, DefaultRunIDMetadataKey, runID)
	store.SetThreadMetadata(threadID, DefaultActiveRunMetadataKey, runID)
}

func (s RunStateService) shouldMutateThreadState(record RunRecord) bool {
	store := s.threadStateStore()
	if store == nil {
		return true
	}
	threadID := strings.TrimSpace(record.ThreadID)
	runID := strings.TrimSpace(record.RunID)
	if threadID == "" || runID == "" {
		return true
	}
	state, ok := store.LoadThreadRuntimeState(threadID)
	if !ok {
		return true
	}
	activeRunID := metadataRunID(state.Metadata[DefaultActiveRunMetadataKey])
	return activeRunID == "" || activeRunID == runID
}

func (s RunStateService) CanMutateThreadState(record RunRecord) bool {
	return s.shouldMutateThreadState(record)
}

func (s RunStateService) clearActiveRunOwnership(record RunRecord) {
	store := s.threadStateStore()
	if store == nil {
		return
	}
	threadID := strings.TrimSpace(record.ThreadID)
	runID := strings.TrimSpace(record.RunID)
	if threadID == "" || runID == "" {
		return
	}
	state, ok := store.LoadThreadRuntimeState(threadID)
	if !ok {
		return
	}
	activeRunID := metadataRunID(state.Metadata[DefaultActiveRunMetadataKey])
	if activeRunID != "" && activeRunID != runID {
		return
	}
	store.ClearThreadMetadata(threadID, DefaultActiveRunMetadataKey)
}
