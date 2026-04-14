package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type CompletionRuntime interface {
	SetThreadTitle(threadID string, title string)
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

	if title := strings.TrimSpace(metadataString(state, s.titleMetadataKey)); title != "" && s.runtime != nil {
		s.runtime.SetThreadTitle(threadID, title)
	}

	decision := s.gate.Evaluate(CompletionGateInput{
		ThreadID:             threadID,
		State:                state,
		Result:               result,
		InterruptMetadataKey: s.interruptMetadataKey,
		PendingTasksKey:      DefaultCompletionPendingTasksKey,
		ExpectedArtifactsKey: DefaultCompletionExpectedArtifactsKey,
	})
	if decision.InterruptRequested {
		interrupt := metadataMap(state, s.interruptMetadataKey)
		if len(interrupt) == 0 && result != nil {
			interrupt = harness.ClarificationInterruptFromMessages(result.Messages)
		}
		if s.runtime != nil {
			s.runtime.SetThreadInterrupts(threadID, []any{interrupt})
			s.runtime.MarkThreadStatus(threadID, "interrupted")
		}
		return CompletionOutcome{
			RunOutcome: decision.Outcome,
			Descriptor: outcomes.Describe(RunRecord{
				ThreadID: threadID,
			}, decision.Outcome, ""),
		}
	}
	if !decision.Allowed {
		if s.runtime != nil {
			s.runtime.ClearThreadInterrupts(threadID)
			s.runtime.MarkThreadStatus(threadID, "idle")
		}
		return CompletionOutcome{
			RunOutcome: decision.Outcome,
			Descriptor: outcomes.Describe(RunRecord{
				ThreadID: threadID,
			}, decision.Outcome, decision.Reason),
		}
	}

	if s.runtime != nil {
		s.runtime.ClearThreadInterrupts(threadID)
		s.runtime.MarkThreadStatus(threadID, "idle")
	}
	return CompletionOutcome{
		RunOutcome: decision.Outcome,
		Descriptor: outcomes.Describe(RunRecord{
			ThreadID: threadID,
		}, decision.Outcome, ""),
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
