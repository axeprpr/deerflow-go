package harnessruntime

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type RunDispatcher interface {
	Prepare(context.Context, *harness.Runtime, RunPlan) (*PreparedExecution, error)
}

type InProcessRunDispatcher struct{}

func NewInProcessRunDispatcher() RunDispatcher {
	return InProcessRunDispatcher{}
}

func (InProcessRunDispatcher) Prepare(ctx context.Context, runtime *harness.Runtime, plan RunPlan) (*PreparedExecution, error) {
	return NewOrchestrator(runtime).Prepare(ctx, plan)
}

