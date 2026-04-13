package harnessruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPRemoteWorkerHandlerBridgesClientAndWorker(t *testing.T) {
	executor := &fakeExecutor{}
	transport := NewDirectWorkerTransportWithResults(executor, DispatchEnvelopeCodec{}, nil)
	server := httptest.NewServer(NewHTTPRemoteWorkerHandler(transport, nil))
	defer server.Close()

	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyRemote,
		Endpoint: server.URL,
	}, DispatchRuntimeConfig{
		Remote:  NewHTTPRemoteWorkerClient(nil),
		Results: DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{ThreadID: "thread-remote", RunID: "run-remote"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-remote" {
		t.Fatalf("executor req = %#v", executor.req)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-remote" {
		t.Fatalf("result = %#v", result)
	}
	if result.Execution.Kind != ExecutionKindLocalPrepared {
		t.Fatalf("result.Execution = %#v", result.Execution)
	}
}

func TestHTTPRemoteWorkerHandlerRejectsBadPayload(t *testing.T) {
	server := httptest.NewServer(NewHTTPRemoteWorkerHandler(NewDirectWorkerTransportWithResults(&fakeExecutor{}, DispatchEnvelopeCodec{}, nil), nil))
	defer server.Close()

	resp, err := http.Post(server.URL, "application/json", strings.NewReader("{bad json"))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
