package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type fakeExecutor struct {
	called bool
	req    DispatchRequest
}

func (e *fakeExecutor) Execute(_ context.Context, req DispatchRequest) (*DispatchResult, error) {
	e.called = true
	e.req = req
	return &DispatchResult{
		Lifecycle: &harness.RunState{ThreadID: req.Plan.ThreadID},
		Execution: &harness.Execution{},
	}, nil
}

func TestInlineDispatchQueueUsesInjectedExecutor(t *testing.T) {
	executor := &fakeExecutor{}
	queue := NewInlineDispatchQueue(executor)

	result, err := queue.Submit(context.Background(), DispatchRequest{
		Plan: RunPlan{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if !executor.called {
		t.Fatal("executor was not called")
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestQueuedRunDispatcherUsesQueue(t *testing.T) {
	executor := &fakeExecutor{}
	dispatcher := NewQueuedRunDispatcher(NewInlineDispatchQueue(executor))

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: RunPlan{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called {
		t.Fatal("executor was not called")
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuntimeDispatcherSupportsDirectTopology(t *testing.T) {
	executor := &fakeExecutor{}
	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyDirect,
		Executor: executor,
	})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: RunPlan{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called {
		t.Fatal("executor was not called")
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-1" {
		t.Fatalf("result = %#v", result)
	}
}
