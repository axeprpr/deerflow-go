package harnessruntime

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type DispatchRequest struct {
	Plan WorkerExecutionPlan
}

type DispatchResult struct {
	Lifecycle *harness.RunState
	Execution *harness.Execution
}

type RunDispatcher interface {
	Dispatch(context.Context, DispatchRequest) (*DispatchResult, error)
}

type DispatchTopology string

const (
	DispatchTopologyDirect DispatchTopology = "direct"
	DispatchTopologyQueued DispatchTopology = "queued"
)

type DispatchConfig struct {
	Topology DispatchTopology
	Executor RunExecutor
	Runtime  func() *harness.Runtime
	Specs    WorkerSpecRuntime
	Codec    WorkerPlanMarshaler
	Queue    DispatchQueue
	Buffer   int
	Workers  int
}

type directRunDispatcher struct {
	executor RunExecutor
}

type queuedRunDispatcher struct {
	queue DispatchQueue
}

func NewInProcessRunDispatcher() RunDispatcher {
	return NewRuntimeDispatcher(DispatchConfig{Topology: DispatchTopologyQueued})
}

func NewRuntimeDispatcher(config DispatchConfig) RunDispatcher {
	executor := config.Executor
	if executor == nil && config.Runtime != nil {
		executor = NewRuntimeWorkerSource(config.Runtime, config.Specs)
	}
	switch config.Topology {
	case DispatchTopologyDirect:
		return directRunDispatcher{executor: executor}
	default:
		queue := config.Queue
		if queue == nil {
			queue = NewInProcessRunQueueWithCodec(executor, config.Buffer, config.Workers, config.Codec)
		}
		return NewQueuedRunDispatcher(queue)
	}
}

func NewQueuedRunDispatcher(queue DispatchQueue) RunDispatcher {
	return queuedRunDispatcher{queue: queue}
}

func (d directRunDispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	executor := d.executor
	if executor == nil {
		executor = NewRuntimeWorker()
	}
	return executor.Execute(ctx, req)
}

func (d queuedRunDispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	queue := d.queue
	if queue == nil {
		queue = NewInProcessRunQueueWithCodec(nil, 0, 0, nil)
	}
	return queue.Submit(ctx, req)
}
