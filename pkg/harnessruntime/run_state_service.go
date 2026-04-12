package harnessruntime

import "time"

type RunStateRuntime interface {
	SaveRunRecord(record RunRecord)
	MarkThreadStatus(threadID string, status string)
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
	record.Status = outcome.RunStatus
	record.Error = outcome.Error
	record.Outcome = outcome
	record.UpdatedAt = s.now()
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
		s.runtime.MarkThreadStatus(record.ThreadID, "error")
	}
	return record
}

func (s RunStateService) MarkCanceled(record RunRecord) RunRecord {
	outcome := NewOutcomeService().Describe(record, RunOutcome{RunStatus: "interrupted", Interrupted: true}, "")
	record.Status = outcome.RunStatus
	record.Error = outcome.Error
	record.Outcome = outcome
	record.UpdatedAt = s.now()
	if s.runtime != nil {
		s.runtime.SaveRunRecord(record)
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
	}
	return record
}
