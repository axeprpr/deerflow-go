package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

const (
	DefaultCompletionPendingTasksKey      = "completion_pending_tasks"
	DefaultCompletionExpectedArtifactsKey = "completion_expected_artifacts"
)

type CompletionGateInput struct {
	ThreadID             string
	State                *harness.RunState
	Result               *agent.RunResult
	InterruptMetadataKey string
	TaskStateMetadataKey string
	PendingTasksKey      string
	ExpectedArtifactsKey string
}

type CompletionGateDecision struct {
	Outcome            RunOutcome
	Allowed            bool
	Reason             string
	PendingTasks       []string
	ExpectedArtifacts  []string
	InterruptRequested bool
}

type CompletionCheck func(CompletionGateInput) (CompletionGateDecision, bool)

type CompletionGate struct {
	checks []CompletionCheck
}

func NewCompletionGate(checks ...CompletionCheck) CompletionGate {
	if len(checks) == 0 {
		checks = []CompletionCheck{
			interruptCompletionCheck,
			taskStateCompletionCheck,
			pendingTasksCompletionCheck,
		}
	}
	return CompletionGate{checks: append([]CompletionCheck(nil), checks...)}
}

func taskStateCompletionCheck(input CompletionGateInput) (CompletionGateDecision, bool) {
	taskState, ok := resolveTaskStateFromRunState(input.State, input.Result, input.TaskStateMetadataKey)
	if !ok {
		return CompletionGateDecision{}, false
	}
	pending := taskState.PendingTexts()
	missing := taskState.MissingExpectedOutputs()
	if len(pending) == 0 && len(missing) == 0 {
		return CompletionGateDecision{}, false
	}
	reason := "pending-tasks"
	if len(pending) == 0 && len(missing) > 0 {
		reason = "missing-expected-outputs"
	}
	return CompletionGateDecision{
		Outcome: RunOutcome{
			RunStatus: "incomplete",
		},
		Allowed:           false,
		Reason:            reason,
		PendingTasks:      pending,
		ExpectedArtifacts: missing,
	}, true
}

func (g CompletionGate) Evaluate(input CompletionGateInput) CompletionGateDecision {
	for _, check := range g.checks {
		if check == nil {
			continue
		}
		if decision, matched := check(input); matched {
			return decision
		}
	}
	return CompletionGateDecision{
		Outcome: NewOutcomeService().Resolve(false),
		Allowed: true,
	}
}

func interruptCompletionCheck(input CompletionGateInput) (CompletionGateDecision, bool) {
	interruptKey := strings.TrimSpace(input.InterruptMetadataKey)
	if interruptKey == "" {
		interruptKey = "clarification_interrupt"
	}
	interrupt := metadataMap(input.State, interruptKey)
	if len(interrupt) == 0 && input.Result != nil {
		interrupt = harness.ClarificationInterruptFromMessages(input.Result.Messages)
	}
	if len(interrupt) == 0 {
		return CompletionGateDecision{}, false
	}
	return CompletionGateDecision{
		Outcome:            NewOutcomeService().Resolve(true),
		Allowed:            true,
		Reason:             "clarification-interrupt",
		InterruptRequested: true,
	}, true
}

func pendingTasksCompletionCheck(input CompletionGateInput) (CompletionGateDecision, bool) {
	pendingKey := strings.TrimSpace(input.PendingTasksKey)
	if pendingKey == "" {
		pendingKey = DefaultCompletionPendingTasksKey
	}
	pending := metadataStringSlice(input.State, pendingKey)
	if len(pending) == 0 {
		return CompletionGateDecision{}, false
	}
	expectedKey := strings.TrimSpace(input.ExpectedArtifactsKey)
	if expectedKey == "" {
		expectedKey = DefaultCompletionExpectedArtifactsKey
	}
	return CompletionGateDecision{
		Outcome: RunOutcome{
			RunStatus: "incomplete",
		},
		Allowed:           false,
		Reason:            "pending-tasks",
		PendingTasks:      pending,
		ExpectedArtifacts: metadataStringSlice(input.State, expectedKey),
	}, true
}

func metadataStringSlice(state *harness.RunState, key string) []string {
	if state == nil || len(state.Metadata) == 0 {
		return nil
	}
	raw, ok := state.Metadata[key]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, _ := item.(string)
			text = strings.TrimSpace(text)
			if text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}
