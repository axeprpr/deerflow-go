package harnessruntime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

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

func TestRuntimeDispatcherQueuedTopologyDefaultsToWorkerQueue(t *testing.T) {
	executor := &fakeExecutor{}
	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyQueued,
		Executor: executor,
	})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: RunPlan{ThreadID: "thread-queued"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called {
		t.Fatal("executor was not called")
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-queued" {
		t.Fatalf("result = %#v", result)
	}
}

type blockingExecutor struct {
	started chan string
	release chan struct{}
	running atomic.Int32
}

func (e *blockingExecutor) Execute(_ context.Context, req DispatchRequest) (*DispatchResult, error) {
	e.running.Add(1)
	e.started <- req.Plan.ThreadID
	<-e.release
	return &DispatchResult{
		Lifecycle: &harness.RunState{ThreadID: req.Plan.ThreadID},
		Execution: &harness.Execution{},
	}, nil
}

func TestInProcessRunQueueSupportsMultipleWorkers(t *testing.T) {
	executor := &blockingExecutor{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	queue := NewInProcessRunQueue(executor, 2, 2)
	defer queue.Close()

	done := make(chan error, 2)
	go func() {
		_, err := queue.Submit(context.Background(), DispatchRequest{Plan: RunPlan{ThreadID: "thread-a"}})
		done <- err
	}()
	go func() {
		_, err := queue.Submit(context.Background(), DispatchRequest{Plan: RunPlan{ThreadID: "thread-b"}})
		done <- err
	}()

	deadline := time.After(200 * time.Millisecond)
	seen := map[string]struct{}{}
	for len(seen) < 2 {
		select {
		case threadID := <-executor.started:
			seen[threadID] = struct{}{}
		case <-deadline:
			t.Fatalf("started workers = %v, want both jobs active", seen)
		}
	}

	close(executor.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}
}
