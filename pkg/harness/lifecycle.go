package harness

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type RunState struct {
	ThreadID         string
	AssistantID      string
	Model            string
	AgentName        string
	Spec             AgentSpec
	TaskState        TaskState
	ExistingMessages []models.Message
	Messages         []models.Message
	Metadata         map[string]any
}

type BeforeRunHook func(context.Context, *RunState) error
type AfterRunHook func(context.Context, *RunState, *agent.RunResult) error

// LifecycleHooks models upstream-style middleware phases without forcing the
// runtime onto a particular middleware implementation yet.
type LifecycleHooks struct {
	BeforeRun []BeforeRunHook
	AfterRun  []AfterRunHook
}

func (l *LifecycleHooks) Before(ctx context.Context, state *RunState) error {
	if l == nil || state == nil {
		return nil
	}
	for _, hook := range l.BeforeRun {
		if hook == nil {
			continue
		}
		if err := hook(ctx, state); err != nil {
			return err
		}
	}
	return nil
}

func (l *LifecycleHooks) After(ctx context.Context, state *RunState, result *agent.RunResult) error {
	if l == nil || state == nil || result == nil {
		return nil
	}
	for _, hook := range l.AfterRun {
		if hook == nil {
			continue
		}
		if err := hook(ctx, state, result); err != nil {
			return err
		}
	}
	return nil
}
