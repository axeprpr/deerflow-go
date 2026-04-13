package harnessruntime

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type fakeExecutor struct {
	called bool
	req    DispatchRequest
}

type fakeRemoteClient struct {
	called   bool
	endpoint string
	request  []byte
	response []byte
	err      error
}

func (e *fakeExecutor) Execute(_ context.Context, req DispatchRequest) (*DispatchResult, error) {
	e.called = true
	e.req = req
	return &DispatchResult{
		Lifecycle: &harness.RunState{ThreadID: req.Plan.ThreadID},
		Handle:    NewStaticExecutionHandle(&harness.Execution{}, req.Plan.ThreadID),
		Execution: ExecutionDescriptor{Kind: ExecutionKindLocalPrepared, SessionID: req.Plan.ThreadID},
	}, nil
}

func (c *fakeRemoteClient) Submit(_ context.Context, endpoint string, payload []byte) ([]byte, error) {
	c.called = true
	c.endpoint = endpoint
	c.request = append([]byte(nil), payload...)
	if c.err != nil {
		return nil, c.err
	}
	return append([]byte(nil), c.response...), nil
}

func TestInlineDispatchQueueUsesInjectedExecutor(t *testing.T) {
	executor := &fakeExecutor{}
	queue := NewDirectWorkerTransport(executor, DispatchEnvelopeCodec{})

	result, err := queue.Submit(context.Background(), WorkerDispatchEnvelope{
		Payload: mustEncodePlan(t, WorkerExecutionPlan{ThreadID: "thread-1"}),
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
	dispatcher := NewQueuedRunDispatcher(NewDirectWorkerTransport(executor, DispatchEnvelopeCodec{}))

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-1"},
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
	}, DispatchRuntimeConfig{Executor: executor})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-1"},
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
	}, DispatchRuntimeConfig{Executor: executor})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-queued"},
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

func TestRuntimeDispatcherRemoteTopologyRequiresEndpoint(t *testing.T) {
	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyRemote,
	}, DispatchRuntimeConfig{})

	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-remote"},
	})
	if err == nil || err.Error() != "remote worker endpoint is required" {
		t.Fatalf("Dispatch() error = %v", err)
	}
}

func TestRuntimeDispatcherRemoteTopologyRequiresClient(t *testing.T) {
	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyRemote,
		Endpoint: "http://worker",
	}, DispatchRuntimeConfig{})

	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-remote"},
	})
	if err == nil || err.Error() != "remote worker client is not configured" {
		t.Fatalf("Dispatch() error = %v", err)
	}
}

func TestRuntimeDispatcherUsesEnvelopeCodec(t *testing.T) {
	executor := &fakeExecutor{}
	codec := &fakePlanCodec{}
	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyDirect,
	}, DispatchRuntimeConfig{Executor: executor, Codec: codec})

	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-codec"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !codec.encoded || !codec.decoded {
		t.Fatalf("codec usage = encode:%v decode:%v", codec.encoded, codec.decoded)
	}
	if !executor.called || executor.req.Plan.ThreadID != "thread-codec" {
		t.Fatalf("executor req = %#v", executor.req)
	}
}

func TestRuntimeDispatcherDirectTopologyUsesResultCodec(t *testing.T) {
	executor := &fakeExecutor{}
	results := &fakeResultCodec{}
	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyDirect,
	}, DispatchRuntimeConfig{Executor: executor, Results: results})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-results"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !results.encoded || !results.decoded {
		t.Fatalf("result codec usage = encode:%v decode:%v", results.encoded, results.decoded)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-results" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuntimeDispatcherRemoteTopologyUsesClientAndResultCodec(t *testing.T) {
	results := &fakeResultCodec{
		last: &DispatchResult{
			Lifecycle: &harness.RunState{ThreadID: "thread-remote"},
			Execution: ExecutionDescriptor{Kind: ExecutionKindLocalPrepared, SessionID: "thread-remote"},
		},
	}
	encodedResult, err := results.Encode(results.last)
	if err != nil {
		t.Fatalf("results.Encode() error = %v", err)
	}
	response, err := json.Marshal(RemoteWorkerResponse{Result: encodedResult})
	if err != nil {
		t.Fatalf("Marshal(response) error = %v", err)
	}
	client := &fakeRemoteClient{response: response}
	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyRemote,
		Endpoint: "http://worker",
	}, DispatchRuntimeConfig{Remote: client, Results: results})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-remote", RunID: "run-remote"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	var request RemoteWorkerRequest
	if err := json.Unmarshal(client.request, &request); err != nil {
		t.Fatalf("json.Unmarshal(client.request) error = %v", err)
	}
	if !client.called || client.endpoint != "http://worker" || request.Envelope.RunID != "run-remote" {
		t.Fatalf("client = %+v", client)
	}
	if !results.decoded {
		t.Fatalf("result codec usage = encode:%v decode:%v", results.encoded, results.decoded)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-remote" {
		t.Fatalf("result = %#v", result)
	}
}

type blockingExecutor struct {
	started chan string
	release chan struct{}
	running atomic.Int32
}

type fakePlanCodec struct {
	encoded bool
	decoded bool
	fail    error
	last    WorkerExecutionPlan
}

type fakeResultCodec struct {
	encoded bool
	decoded bool
	fail    error
	last    *DispatchResult
}

func (c *fakePlanCodec) Encode(plan WorkerExecutionPlan) ([]byte, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	c.encoded = true
	c.last = plan
	return []byte(plan.ThreadID), nil
}

func (c *fakePlanCodec) Decode(data []byte) (WorkerExecutionPlan, error) {
	if c.fail != nil {
		return WorkerExecutionPlan{}, c.fail
	}
	c.decoded = true
	return c.last, nil
}

func (c *fakeResultCodec) Encode(result *DispatchResult) ([]byte, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	c.encoded = true
	c.last = result
	return []byte("result"), nil
}

func (c *fakeResultCodec) Decode(data []byte) (*DispatchResult, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	c.decoded = true
	if c.last != nil {
		return c.last, nil
	}
	return &DispatchResult{}, nil
}

func (e *blockingExecutor) Execute(_ context.Context, req DispatchRequest) (*DispatchResult, error) {
	e.running.Add(1)
	e.started <- req.Plan.ThreadID
	<-e.release
	return &DispatchResult{
		Lifecycle: &harness.RunState{ThreadID: req.Plan.ThreadID},
		Handle:    NewStaticExecutionHandle(&harness.Execution{}, req.Plan.ThreadID),
		Execution: ExecutionDescriptor{Kind: ExecutionKindLocalPrepared, SessionID: req.Plan.ThreadID},
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
		_, err := queue.Submit(context.Background(), WorkerDispatchEnvelope{Payload: mustEncodePlan(t, WorkerExecutionPlan{ThreadID: "thread-a"})})
		done <- err
	}()
	go func() {
		_, err := queue.Submit(context.Background(), WorkerDispatchEnvelope{Payload: mustEncodePlan(t, WorkerExecutionPlan{ThreadID: "thread-b"})})
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

func TestInProcessRunQueueUsesWorkerPlanCodec(t *testing.T) {
	executor := &fakeExecutor{}
	codec := &fakePlanCodec{last: WorkerExecutionPlan{ThreadID: "thread-codec"}}
	queue := NewInProcessRunQueueWithCodec(executor, 1, 1, codec, nil)
	defer queue.Close()

	_, err := queue.Submit(context.Background(), WorkerDispatchEnvelope{Payload: []byte("thread-codec")})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if codec.encoded || !codec.decoded {
		t.Fatalf("codec usage = encode:%v decode:%v", codec.encoded, codec.decoded)
	}
	if !executor.called || executor.req.Plan.ThreadID != "thread-codec" {
		t.Fatalf("executor req = %#v", executor.req)
	}
}

func TestInProcessRunQueueReturnsCodecErrors(t *testing.T) {
	queue := NewInProcessRunQueueWithCodec(&fakeExecutor{}, 1, 1, &fakePlanCodec{fail: errors.New("boom")}, nil)
	defer queue.Close()

	_, err := queue.Submit(context.Background(), WorkerDispatchEnvelope{Payload: []byte("thread-codec")})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("Submit() error = %v, want boom", err)
	}
}

func TestInProcessRunQueuePreservesDispatchEnvelopeMetadata(t *testing.T) {
	executor := &fakeExecutor{}
	queue := NewInProcessRunQueueWithCodec(executor, 1, 1, nil, nil)
	defer queue.Close()

	_, err := queue.Submit(context.Background(), WorkerDispatchEnvelope{
		RunID:    "run-1",
		ThreadID: "thread-1",
		Attempt:  3,
		Payload: mustEncodePlan(t, WorkerExecutionPlan{
			RunID:    "run-1",
			ThreadID: "thread-1",
			Attempt:  3,
		}),
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-1" || executor.req.Plan.Attempt != 3 {
		t.Fatalf("executor req = %#v", executor.req)
	}
}

func mustEncodePlan(t *testing.T, plan WorkerExecutionPlan) []byte {
	t.Helper()
	payload, err := (WorkerPlanCodec{}).Encode(plan)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	return payload
}
