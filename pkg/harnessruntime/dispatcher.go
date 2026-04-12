package harnessruntime

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type DispatchRequest struct {
	Runtime *harness.Runtime
	Plan    RunPlan
}

type DispatchResult struct {
	Lifecycle *harness.RunState
	Execution *harness.Execution
}

type RunDispatcher interface {
	Dispatch(context.Context, DispatchRequest) (*DispatchResult, error)
}

type InProcessRunDispatcher struct{}

func NewInProcessRunDispatcher() RunDispatcher {
	return InProcessRunDispatcher{}
}

func (InProcessRunDispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	prepared, err := NewOrchestrator(req.Runtime).Prepare(ctx, req.Plan)
	if err != nil {
		return nil, err
	}
	return &DispatchResult{
		Lifecycle: prepared.Lifecycle,
		Execution: prepared.Execution,
	}, nil
}
