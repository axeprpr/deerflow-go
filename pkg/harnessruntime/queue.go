package harnessruntime

import "context"

type DispatchQueue interface {
	Submit(context.Context, DispatchRequest) (*DispatchResult, error)
}

type InlineDispatchQueue struct {
	executor RunExecutor
}

func NewInlineDispatchQueue(executor RunExecutor) DispatchQueue {
	return InlineDispatchQueue{executor: executor}
}

func (q InlineDispatchQueue) Submit(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	executor := q.executor
	if executor == nil {
		executor = NewRuntimeWorker()
	}
	return executor.Execute(ctx, req)
}
