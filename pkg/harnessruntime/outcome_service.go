package harnessruntime

type RunOutcome struct {
	Interrupted bool
	RunStatus   string
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
