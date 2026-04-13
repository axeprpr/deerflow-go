package harnessruntime

import (
	"context"
	"errors"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type RunExecutor interface {
	Execute(context.Context, DispatchRequest) (*DispatchResult, error)
}

type RuntimeWorker struct {
	runtime  func() *harness.Runtime
	specs    WorkerSpecRuntime
	complete bool
}

func NewRuntimeWorker() RunExecutor {
	return RuntimeWorker{}
}

func NewRuntimeWorkerSource(source func() *harness.Runtime, specs WorkerSpecRuntime) RunExecutor {
	return RuntimeWorker{runtime: source, specs: specs}
}

func NewCompletingRuntimeWorkerSource(source func() *harness.Runtime, specs WorkerSpecRuntime) RunExecutor {
	return RuntimeWorker{runtime: source, specs: specs, complete: true}
}

func (w RuntimeWorker) Execute(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	return w.execute(ctx, req)
}

func (w RuntimeWorker) execute(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	var runtime *harness.Runtime
	if w.runtime != nil {
		runtime = w.runtime()
	}
	if runtime == nil {
		return nil, errors.New("runtime is required")
	}
	prepared, err := NewOrchestrator(runtime, w.specs).PrepareExecution(ctx, req.Plan)
	if err != nil {
		return nil, err
	}
	handle := NewStaticExecutionHandle(prepared.Execution, prepared.Lifecycle.ThreadID)
	if w.complete {
		result, err := prepared.Execution.Run(ctx)
		if err != nil {
			return nil, err
		}
		return &DispatchResult{
			Lifecycle: prepared.Lifecycle,
			Execution: ExecutionDescriptor{
				Kind:      ExecutionKindRemoteCompleted,
				SessionID: prepared.Lifecycle.ThreadID,
			},
			Completed: result,
		}, nil
	}
	return &DispatchResult{
		Lifecycle: prepared.Lifecycle,
		Handle:    handle,
		Execution: handle.Describe(),
	}, nil
}
