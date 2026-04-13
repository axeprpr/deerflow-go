package harnessruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultRuntimeNodeConfigUsesQueuedLocalDefaults(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", "/tmp/runtime-test")
	if config.Sandbox.Backend != SandboxBackendLocalLinux {
		t.Fatalf("sandbox backend = %q, want %q", config.Sandbox.Backend, SandboxBackendLocalLinux)
	}
	if config.Transport.Backend != WorkerTransportBackendQueue {
		t.Fatalf("transport backend = %q, want %q", config.Transport.Backend, WorkerTransportBackendQueue)
	}
	if config.State.Backend != RuntimeStateStoreBackendInMemory {
		t.Fatalf("state backend = %q, want %q", config.State.Backend, RuntimeStateStoreBackendInMemory)
	}
	if config.State.SnapshotBackend != RuntimeStateStoreBackendInMemory || config.State.EventBackend != RuntimeStateStoreBackendInMemory || config.State.ThreadBackend != RuntimeStateStoreBackendInMemory {
		t.Fatalf("state config = %+v", config.State)
	}
}

func TestRuntimeNodeConfigBuildsFileBackedStateStores(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	root := t.TempDir()
	config.State = RuntimeStateStoreConfig{
		Backend: RuntimeStateStoreBackendFile,
		Root:    root,
	}
	runStore, ok := config.BuildRunSnapshotStore().(*JSONFileRunStore)
	if !ok {
		t.Fatalf("BuildRunSnapshotStore() returned %T", config.BuildRunSnapshotStore())
	}
	threadStore, ok := config.BuildThreadStateStore().(*JSONFileThreadStateStore)
	if !ok {
		t.Fatalf("BuildThreadStateStore() returned %T", config.BuildThreadStateStore())
	}
	if runStore.root != filepath.Join(root, "runs") {
		t.Fatalf("runStore.root = %q", runStore.root)
	}
	if threadStore.root != filepath.Join(root, "threads") {
		t.Fatalf("threadStore.root = %q", threadStore.root)
	}
	if eventStore, ok := config.BuildRunEventStore().(*JSONFileRunEventStore); !ok {
		t.Fatalf("BuildRunEventStore() returned %T", config.BuildRunEventStore())
	} else if eventStore.root != filepath.Join(root, "events") {
		t.Fatalf("eventStore.root = %q", eventStore.root)
	}
}

func TestRuntimeNodeConfigSupportsMixedStateBackends(t *testing.T) {
	root := t.TempDir()
	config := DefaultRuntimeNodeConfig("runtime-test", root)
	config.State = RuntimeStateStoreConfig{
		SnapshotBackend: RuntimeStateStoreBackendFile,
		EventBackend:    RuntimeStateStoreBackendInMemory,
		ThreadBackend:   RuntimeStateStoreBackendInMemory,
		Root:            root,
	}
	if _, ok := config.BuildRunSnapshotStore().(*JSONFileRunStore); !ok {
		t.Fatalf("BuildRunSnapshotStore() returned %T", config.BuildRunSnapshotStore())
	}
	if _, ok := config.BuildThreadStateStore().(*InMemoryThreadStateStore); !ok {
		t.Fatalf("BuildThreadStateStore() returned %T", config.BuildThreadStateStore())
	}
	if _, ok := config.BuildRunEventStore().(*InMemoryRunEventStore); !ok {
		t.Fatalf("BuildRunEventStore() returned %T", config.BuildRunEventStore())
	}
}

func TestRuntimeNodeConfigBuildStatePlane(t *testing.T) {
	root := t.TempDir()
	config := DefaultRuntimeNodeConfig("runtime-test", root)
	config.State = RuntimeStateStoreConfig{
		SnapshotBackend: RuntimeStateStoreBackendFile,
		EventBackend:    RuntimeStateStoreBackendFile,
		ThreadBackend:   RuntimeStateStoreBackendInMemory,
		Root:            root,
	}
	plane := config.BuildStatePlane()
	if _, ok := plane.Snapshots.(*JSONFileRunStore); !ok {
		t.Fatalf("Snapshots = %T", plane.Snapshots)
	}
	if _, ok := plane.Events.(*JSONFileRunEventStore); !ok {
		t.Fatalf("Events = %T", plane.Events)
	}
	if _, ok := plane.Threads.(*InMemoryThreadStateStore); !ok {
		t.Fatalf("Threads = %T", plane.Threads)
	}
}

func TestRuntimeNodeConfigBuildWorkerTransportFallsBackFromRemoteToLocalWorker(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	config.Transport = WorkerTransportConfig{
		Backend:  WorkerTransportBackendRemote,
		Endpoint: "http://should-not-be-used",
	}
	executor := &fakeExecutor{}
	transport := config.BuildWorkerTransport(DispatchRuntimeConfig{
		Executor: executor,
		Results:  DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})

	result, err := transport.Submit(context.Background(), WorkerDispatchEnvelope{
		Payload: mustEncodePlan(t, WorkerExecutionPlan{RunID: "run-1", ThreadID: "thread-1"}),
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-1" {
		t.Fatalf("executor req = %#v", executor.req)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuntimeNodeConfigBuildRemoteWorkerHandlerServesDispatchRequests(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	config.Transport.Backend = WorkerTransportBackendRemote
	executor := &fakeExecutor{}
	handler := config.BuildRemoteWorkerHandler(DispatchRuntimeConfig{
		Executor: executor,
		Results:  DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyRemote,
		Endpoint: server.URL + DefaultRemoteWorkerDispatchPath,
	}, DispatchRuntimeConfig{
		Remote:  NewHTTPRemoteWorkerClient(nil),
		Results: DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{RunID: "run-2", ThreadID: "thread-2"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-2" {
		t.Fatalf("executor req = %#v", executor.req)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-2" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuntimeNodeConfigBuildRemoteWorkerHTTPServerUsesConfiguredAddress(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	config.RemoteWorker = RemoteWorkerServerConfig{
		Addr:              "127.0.0.1:19081",
		ReadHeaderTimeout: 3 * time.Second,
	}
	server := config.BuildRemoteWorkerHTTPServer(DispatchRuntimeConfig{
		Executor: &fakeExecutor{},
	})
	if server.Addr != "127.0.0.1:19081" {
		t.Fatalf("server.Addr = %q", server.Addr)
	}
	if server.ReadHeaderTimeout != 3*time.Second {
		t.Fatalf("server.ReadHeaderTimeout = %v", server.ReadHeaderTimeout)
	}
	if server.Handler == nil {
		t.Fatal("server.Handler is nil")
	}
}

func TestRuntimeNodeConfigBuildRemoteWorkerNodeWrapsHTTPServer(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	node := config.BuildRemoteWorkerNode(DispatchRuntimeConfig{
		Executor: &fakeExecutor{},
	})
	if node == nil || node.Server() == nil {
		t.Fatalf("node = %#v", node)
	}
}

func TestHTTPRemoteWorkerNodeStartRequiresServer(t *testing.T) {
	node := NewHTTPRemoteWorkerNode(nil)
	err := node.Start()
	if err == nil || err.Error() != "remote worker server is not configured" {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestHTTPRemoteWorkerNodeShutdownDelegatesToServer(t *testing.T) {
	node := NewHTTPRemoteWorkerNode(&http.Server{})
	if err := node.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}
