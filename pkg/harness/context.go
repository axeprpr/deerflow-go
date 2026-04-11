package harness

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type RunHooks struct {
	TaskSink          subagent.EventSink
	ClarificationSink clarification.EventSink
}

// ContextSpec describes compat-owned adaptation data that the harness runtime
// binds into one execution context.
type ContextSpec struct {
	ThreadID             string
	ClarificationManager *clarification.Manager
	RuntimeContext       map[string]any
	Hooks                RunHooks
}

func BindContext(ctx context.Context, spec ContextSpec) context.Context {
	runCtx := subagent.WithEventSink(ctx, spec.Hooks.TaskSink)
	runCtx = clarification.WithThreadID(runCtx, spec.ThreadID)
	runCtx = clarification.WithManager(runCtx, spec.ClarificationManager)
	runCtx = clarification.WithEventSink(runCtx, spec.Hooks.ClarificationSink)
	runCtx = tools.WithRuntimeContext(runCtx, spec.RuntimeContext)
	return runCtx
}
