package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type CompletionRuntime interface {
	SetThreadTitle(threadID string, title string)
	LoadThreadTaskState(threadID string) harness.TaskState
	SetThreadTaskState(threadID string, taskState harness.TaskState)
	ClearThreadTaskState(threadID string)
	SetThreadTaskLifecycle(threadID string, lifecycle TaskLifecycleDescriptor)
	ClearThreadTaskLifecycle(threadID string)
	SetThreadInterrupts(threadID string, interrupts []any)
	ClearThreadInterrupts(threadID string)
	MarkThreadStatus(threadID string, status string)
}

type CompletionService struct {
	runtime              CompletionRuntime
	titleMetadataKey     string
	interruptMetadataKey string
	gate                 CompletionGate
}

type CompletionOutcome struct {
	RunOutcome
	Descriptor RunOutcomeDescriptor
}

func NewCompletionService(runtime CompletionRuntime, titleMetadataKey string, interruptMetadataKey string) CompletionService {
	if strings.TrimSpace(titleMetadataKey) == "" {
		titleMetadataKey = "generated_title"
	}
	if strings.TrimSpace(interruptMetadataKey) == "" {
		interruptMetadataKey = "clarification_interrupt"
	}
	return CompletionService{
		runtime:              runtime,
		titleMetadataKey:     titleMetadataKey,
		interruptMetadataKey: interruptMetadataKey,
		gate:                 NewCompletionGate(),
	}
}

func (s CompletionService) Apply(threadID string, state *harness.RunState, result *agent.RunResult) CompletionOutcome {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return CompletionOutcome{}
	}
	outcomes := NewOutcomeService()

	taskState := harness.TaskState{}
	if title := strings.TrimSpace(metadataString(state, s.titleMetadataKey)); title != "" && s.runtime != nil {
		s.runtime.SetThreadTitle(threadID, title)
	}
	if s.runtime != nil {
		taskState = s.runtime.LoadThreadTaskState(threadID)
	}
	if resolved, ok := resolveTaskStateFromRunState(state, result, DefaultTaskStateMetadataKey); ok {
		if merged, err := mergeTaskProgressState(taskState, resolved); err == nil {
			taskState = merged
		} else {
			taskState = resolved
		}
	} else if taskState.IsZero() && state != nil {
		taskState = state.TaskState
	}
	if state != nil {
		state.TaskState = taskState
	}
	if s.runtime != nil {
		if !taskState.IsZero() {
			s.runtime.SetThreadTaskState(threadID, taskState)
		} else {
			s.runtime.ClearThreadTaskState(threadID)
		}
	}

	decision := s.gate.Evaluate(CompletionGateInput{
		ThreadID:             threadID,
		State:                state,
		Result:               result,
		InterruptMetadataKey: s.interruptMetadataKey,
		TaskStateMetadataKey: DefaultTaskStateMetadataKey,
		PendingTasksKey:      DefaultCompletionPendingTasksKey,
		ExpectedArtifactsKey: DefaultCompletionExpectedArtifactsKey,
	})
	if decision.InterruptRequested {
		interrupt := metadataMap(state, s.interruptMetadataKey)
		if len(interrupt) == 0 && result != nil {
			interrupt = harness.ClarificationInterruptFromMessages(result.Messages)
		}
		descriptor := outcomes.DescribeWithTaskState(RunRecord{
			ThreadID: threadID,
		}, decision.Outcome, "", taskState, true)
		lifecycle := descriptor.TaskLifecycle
		if s.runtime != nil {
			s.runtime.SetThreadInterrupts(threadID, []any{interrupt})
			s.runtime.SetThreadTaskLifecycle(threadID, lifecycle)
			s.runtime.MarkThreadStatus(threadID, "interrupted")
		}
		return CompletionOutcome{
			RunOutcome: decision.Outcome,
			Descriptor: descriptor,
		}
	}
	if !decision.Allowed {
		descriptor := outcomes.DescribeWithTaskState(RunRecord{
			ThreadID: threadID,
		}, decision.Outcome, decision.Reason, taskState, false)
		lifecycle := descriptor.TaskLifecycle
		lifecycle.PendingTasks = append([]string(nil), decision.PendingTasks...)
		lifecycle.ExpectedArtifacts = append([]string(nil), decision.ExpectedArtifacts...)
		if s.runtime != nil {
			s.runtime.ClearThreadInterrupts(threadID)
			s.runtime.SetThreadTaskLifecycle(threadID, lifecycle)
			s.runtime.MarkThreadStatus(threadID, "idle")
		}
		descriptor.PendingTasks = append([]string(nil), decision.PendingTasks...)
		descriptor.ExpectedArtifacts = append([]string(nil), decision.ExpectedArtifacts...)
		descriptor.TaskLifecycle = lifecycle
		return CompletionOutcome{
			RunOutcome: decision.Outcome,
			Descriptor: descriptor,
		}
	}

	descriptor := outcomes.DescribeWithTaskState(RunRecord{
		ThreadID: threadID,
	}, decision.Outcome, "", taskState, false)
	lifecycle := descriptor.TaskLifecycle
	if s.runtime != nil {
		s.runtime.ClearThreadInterrupts(threadID)
		s.runtime.SetThreadTaskLifecycle(threadID, lifecycle)
		s.runtime.MarkThreadStatus(threadID, "idle")
	}
	return CompletionOutcome{
		RunOutcome: decision.Outcome,
		Descriptor: descriptor,
	}
}

func metadataString(state *harness.RunState, key string) string {
	if state == nil || len(state.Metadata) == 0 {
		return ""
	}
	raw, _ := state.Metadata[key].(string)
	return strings.TrimSpace(raw)
}

func metadataMap(state *harness.RunState, key string) map[string]any {
	if state == nil || len(state.Metadata) == 0 {
		return nil
	}
	value, _ := state.Metadata[key].(map[string]any)
	if len(value) == 0 {
		return nil
	}
	return value
}
