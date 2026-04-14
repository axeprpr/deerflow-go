package harnessruntime

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type RunStateRuntime interface {
	SaveRunRecord(record RunRecord)
	MarkThreadStatus(threadID string, status string)
	SetThreadTaskLifecycle(threadID string, lifecycle TaskLifecycleDescriptor)
	ClearThreadTaskLifecycle(threadID string)
}

type RunStateService struct {
	runtime RunStateRuntime
	now     func() time.Time
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
	outcome := NewOutcomeService().Describe(record, RunOutcome{RunStatus: "error"}, err.Error())
	outcome.TaskLifecycle = NewTaskLifecycleService().Describe(RunOutcome{RunStatus: "error"}, harness.TaskState{}, false)
	record.Status = outcome.RunStatus
	record.Error = outcome.Error
	record.Outcome = outcome
	record.UpdatedAt = s.now()
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		s.runtime.SetThreadTaskLifecycle(record.ThreadID, outcome.TaskLifecycle)
		s.runtime.MarkThreadStatus(record.ThreadID, "error")
	}
	return record
}

func (s RunStateService) Begin(record RunRecord) RunRecord {
	if record.Status == "" {
		record.Status = "running"
	}
	if record.Outcome.RunStatus == "" {
		record.Outcome = NewOutcomeService().Describe(record, RunOutcome{RunStatus: "running"}, "")
	}
	record.Outcome.TaskLifecycle = NewTaskLifecycleService().Describe(RunOutcome{RunStatus: "running"}, harness.TaskState{}, false)
	record.UpdatedAt = s.now()
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		s.runtime.SetThreadTaskLifecycle(record.ThreadID, record.Outcome.TaskLifecycle)
		s.runtime.MarkThreadStatus(record.ThreadID, "busy")
	}
	return record
}

func (s RunStateService) MarkCanceled(record RunRecord) RunRecord {
	outcome := NewOutcomeService().Describe(record, RunOutcome{RunStatus: "interrupted", Interrupted: true}, "")
	outcome.TaskLifecycle = NewTaskLifecycleService().Describe(RunOutcome{RunStatus: "interrupted", Interrupted: true}, harness.TaskState{}, false)
	record.Status = outcome.RunStatus
	record.Error = outcome.Error
	record.Outcome = outcome
	record.UpdatedAt = s.now()
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		s.runtime.SetThreadTaskLifecycle(record.ThreadID, outcome.TaskLifecycle)
		s.runtime.MarkThreadStatus(record.ThreadID, "interrupted")
	}
	return record
}

func (s RunStateService) Finalize(record RunRecord, outcome CompletionOutcome) RunRecord {
	record.Status = outcome.Descriptor.RunStatus
	record.Error = outcome.Descriptor.Error
	record.Outcome = outcome.Descriptor
	record.UpdatedAt = s.now()
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		if !record.Outcome.TaskLifecycle.IsZero() {
			s.runtime.SetThreadTaskLifecycle(record.ThreadID, record.Outcome.TaskLifecycle)
		} else {
			s.runtime.ClearThreadTaskLifecycle(record.ThreadID)
		}
	}
	return record
}
