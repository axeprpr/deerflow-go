package harnessruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRemoteWorkerServerHealthEndpoint(t *testing.T) {
	server := httptest.NewServer(NewHTTPRemoteWorkerServer(NewDirectWorkerTransportWithResults(&fakeExecutor{}, DispatchEnvelopeCodec{}, nil), nil).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + DefaultRemoteWorkerHealthPath)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %#v", body)
	}
}

func TestHTTPRemoteWorkerServerDispatchEndpoint(t *testing.T) {
	executor := &fakeExecutor{}
	server := httptest.NewServer(NewHTTPRemoteWorkerServer(NewDirectWorkerTransportWithResults(executor, DispatchEnvelopeCodec{}, nil), nil).Handler())
	defer server.Close()

	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyRemote,
		Endpoint: server.URL + DefaultRemoteWorkerDispatchPath,
	}, DispatchRuntimeConfig{
		Remote:  NewHTTPRemoteWorkerClient(nil),
		Results: DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{RunID: "run-1", ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-1" {
		t.Fatalf("executor req = %#v", executor.req)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-1" {
		t.Fatalf("result = %#v", result)
	}
}
