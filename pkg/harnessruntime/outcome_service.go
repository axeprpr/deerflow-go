package harnessruntime

import "github.com/axeprpr/deerflow-go/pkg/harness"

type RunOutcome struct {
	Interrupted bool
	RunStatus   string
}

type RunOutcomeDescriptor struct {
	RunStatus         string
	Interrupted       bool
	Error             string
	PendingTasks      []string
	ExpectedArtifacts []string
	TaskLifecycle     TaskLifecycleDescriptor
	Attempt           int
	ResumeFromEvent   int
	ResumeReason      string
}

type OutcomeService struct{}

func NewOutcomeService() OutcomeService {
	return OutcomeService{}
}

func (OutcomeService) Resolve(interrupted bool) RunOutcome {
	if interrupted {
		return RunOutcome{
			Interrupted: true,
			RunStatus:   "interrupted",
		}
	}
	return RunOutcome{
		Interrupted: false,
		RunStatus:   "success",
	}
}

func (s OutcomeService) Describe(record RunRecord, outcome RunOutcome, errText string) RunOutcomeDescriptor {
	return RunOutcomeDescriptor{
		RunStatus:       outcome.RunStatus,
		Interrupted:     outcome.Interrupted,
		Error:           errText,
		TaskLifecycle:   NewTaskLifecycleService().Describe(outcome, harness.TaskState{}, false),
		Attempt:         record.Attempt,
		ResumeFromEvent: record.ResumeFromEvent,
		ResumeReason:    record.ResumeReason,
	}
}

func (s OutcomeService) BindRecord(record RunRecord, descriptor RunOutcomeDescriptor) RunOutcomeDescriptor {
	descriptor.Attempt = record.Attempt
	descriptor.ResumeFromEvent = record.ResumeFromEvent
	descriptor.ResumeReason = record.ResumeReason
	return descriptor
}

func (s OutcomeService) DescribeRunning(record RunRecord) RunOutcomeDescriptor {
	return s.BindRecord(record, RunOutcomeDescriptor{
		RunStatus: "running",
		TaskLifecycle: NewTaskLifecycleService().Describe(
			RunOutcome{RunStatus: "running"},
			harness.TaskState{},
			false,
		),
	})
}
