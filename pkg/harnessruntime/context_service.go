package harnessruntime

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type ContextRuntime interface {
	ClarificationManager() *clarification.Manager
	SkillPaths() any
}

type ContextBinder interface {
	BindContext(ctx context.Context, spec harness.ContextSpec) context.Context
}

type ContextService struct {
	runtime ContextRuntime
	binder  ContextBinder
}

func NewContextService(runtime ContextRuntime, binder ContextBinder) ContextService {
	return ContextService{
		runtime: runtime,
		binder:  binder,
	}
}

func (s ContextService) Spec(threadID string, hooks harness.RunHooks) harness.ContextSpec {
	spec := harness.ContextSpec{
		ThreadID: threadID,
		Hooks:    hooks,
	}
	if s.runtime != nil {
		spec.ClarificationManager = s.runtime.ClarificationManager()
		spec.RuntimeContext = map[string]any{
			"skill_paths": s.runtime.SkillPaths(),
		}
	}
	return spec
}

func (s ContextService) Bind(ctx context.Context, threadID string, hooks harness.RunHooks) context.Context {
	if s.binder == nil {
		return ctx
	}
	return s.binder.BindContext(ctx, s.Spec(threadID, hooks))
}
