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

type queuedRunDispatcher struct {
	queue DispatchQueue
}

func NewInProcessRunDispatcher() RunDispatcher {
	return NewQueuedRunDispatcher(nil)
}

func NewQueuedRunDispatcher(queue DispatchQueue) RunDispatcher {
	return queuedRunDispatcher{queue: queue}
}

func (d queuedRunDispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	queue := d.queue
	if queue == nil {
		queue = NewInlineDispatchQueue(nil)
	}
	return queue.Submit(ctx, req)
}
