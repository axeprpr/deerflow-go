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
	runtime func() *harness.Runtime
}

func NewRuntimeWorker() RunExecutor {
	return RuntimeWorker{}
}

func NewRuntimeWorkerSource(source func() *harness.Runtime) RunExecutor {
	return RuntimeWorker{runtime: source}
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
	prepared, err := NewOrchestrator(runtime).Prepare(ctx, req.Plan)
	if err != nil {
		return nil, err
	}
	return &DispatchResult{
		Lifecycle: prepared.Lifecycle,
		Execution: prepared.Execution,
	}, nil
}
