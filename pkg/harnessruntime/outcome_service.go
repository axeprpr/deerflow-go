package harnessruntime

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
		Attempt:         record.Attempt,
		ResumeFromEvent: record.ResumeFromEvent,
		ResumeReason:    record.ResumeReason,
	}
}
