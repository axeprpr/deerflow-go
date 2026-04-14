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
	TaskState         harness.TaskState
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
		TaskState:       harness.TaskState{},
		TaskLifecycle:   NewTaskLifecycleService().Describe(outcome, harness.TaskState{}, false),
		Attempt:         record.Attempt,
		ResumeFromEvent: record.ResumeFromEvent,
		ResumeReason:    record.ResumeReason,
	}
}

func (s OutcomeService) DescribeWithTaskState(record RunRecord, outcome RunOutcome, errText string, taskState harness.TaskState, paused bool) RunOutcomeDescriptor {
	descriptor := s.Describe(record, outcome, errText)
	descriptor.TaskState = taskState
	descriptor.TaskLifecycle = NewTaskLifecycleService().Describe(outcome, taskState, paused)
	descriptor.PendingTasks = append([]string(nil), descriptor.TaskLifecycle.PendingTasks...)
	descriptor.ExpectedArtifacts = append([]string(nil), descriptor.TaskLifecycle.ExpectedArtifacts...)
	return descriptor
}

func (s OutcomeService) BindRecord(record RunRecord, descriptor RunOutcomeDescriptor) RunOutcomeDescriptor {
	if len(descriptor.PendingTasks) == 0 && len(descriptor.TaskLifecycle.PendingTasks) > 0 {
		descriptor.PendingTasks = append([]string(nil), descriptor.TaskLifecycle.PendingTasks...)
	}
	if len(descriptor.ExpectedArtifacts) == 0 && len(descriptor.TaskLifecycle.ExpectedArtifacts) > 0 {
		descriptor.ExpectedArtifacts = append([]string(nil), descriptor.TaskLifecycle.ExpectedArtifacts...)
	}
	descriptor.Attempt = record.Attempt
	descriptor.ResumeFromEvent = record.ResumeFromEvent
	descriptor.ResumeReason = record.ResumeReason
	return descriptor
}

func (s OutcomeService) DescribeRunning(record RunRecord) RunOutcomeDescriptor {
	return s.DescribeWithTaskState(record, RunOutcome{RunStatus: "running"}, "", harness.TaskState{}, false)
}

func LoadLiveTaskState(store ThreadStateStore, threadID string) harness.TaskState {
	if store == nil {
		return harness.TaskState{}
	}
	state, ok := store.LoadThreadRuntimeState(threadID)
	if !ok {
		return harness.TaskState{}
	}
	taskState, ok := harness.ParseTaskState(state.Metadata[DefaultTaskStateMetadataKey])
	if !ok {
		return harness.TaskState{}
	}
	return taskState
}

func LoadLiveTaskLifecycle(store ThreadStateStore, threadID string) (TaskLifecycleDescriptor, bool) {
	if store == nil {
		return TaskLifecycleDescriptor{}, false
	}
	state, ok := store.LoadThreadRuntimeState(threadID)
	if !ok {
		return TaskLifecycleDescriptor{}, false
	}
	return ParseTaskLifecycle(state.Metadata[DefaultTaskLifecycleMetadataKey])
}

func (s OutcomeService) DescribeLiveRunning(record RunRecord, store ThreadStateStore) RunOutcomeDescriptor {
	taskState := LoadLiveTaskState(store, record.ThreadID)
	outcome := s.DescribeWithTaskState(record, RunOutcome{RunStatus: "running"}, "", taskState, false)
	if !taskState.IsZero() {
		return outcome
	}
	if lifecycle, ok := LoadLiveTaskLifecycle(store, record.ThreadID); ok {
		outcome.TaskLifecycle = lifecycle
		outcome.PendingTasks = append([]string(nil), lifecycle.PendingTasks...)
		outcome.ExpectedArtifacts = append([]string(nil), lifecycle.ExpectedArtifacts...)
	}
	return outcome
}
