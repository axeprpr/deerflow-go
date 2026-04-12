package harnessruntime

import "context"

type RunExecutor interface {
	Execute(context.Context, DispatchRequest) (*DispatchResult, error)
}

type RuntimeWorker struct{}

func NewRuntimeWorker() RunExecutor {
	return RuntimeWorker{}
}

func (RuntimeWorker) Execute(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	prepared, err := NewOrchestrator(req.Runtime).Prepare(ctx, req.Plan)
	if err != nil {
		return nil, err
	}
	return &DispatchResult{
		Lifecycle: prepared.Lifecycle,
		Execution: prepared.Execution,
	}, nil
}
