package harnessruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultRuntimeNodeConfigUsesQueuedLocalDefaults(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", "/tmp/runtime-test")
	if config.Role != RuntimeNodeRoleAllInOne {
		t.Fatalf("role = %q, want %q", config.Role, RuntimeNodeRoleAllInOne)
	}
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

func TestDefaultGatewayRuntimeNodeConfigUsesRemoteTransport(t *testing.T) {
	config := DefaultGatewayRuntimeNodeConfig("runtime-test", "/tmp/runtime-test", "http://worker:8081/dispatch")
	if config.Role != RuntimeNodeRoleGateway {
		t.Fatalf("role = %q, want %q", config.Role, RuntimeNodeRoleGateway)
	}
	if config.Transport.Backend != WorkerTransportBackendRemote {
		t.Fatalf("transport backend = %q, want %q", config.Transport.Backend, WorkerTransportBackendRemote)
	}
	if config.Transport.Endpoint != "http://worker:8081/dispatch" {
		t.Fatalf("transport endpoint = %q", config.Transport.Endpoint)
	}
}

func TestDefaultWorkerRuntimeNodeConfigUsesQueuedTransport(t *testing.T) {
	config := DefaultWorkerRuntimeNodeConfig("runtime-test", "/tmp/runtime-test")
	if config.Role != RuntimeNodeRoleWorker {
		t.Fatalf("role = %q, want %q", config.Role, RuntimeNodeRoleWorker)
	}
	if config.Transport.Backend != WorkerTransportBackendQueue {
		t.Fatalf("transport backend = %q, want %q", config.Transport.Backend, WorkerTransportBackendQueue)
	}
	if config.Transport.Endpoint != "" {
		t.Fatalf("transport endpoint = %q, want empty", config.Transport.Endpoint)
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

func TestGatewayRuntimeNodeDoesNotExposeRemoteWorkerServer(t *testing.T) {
	config := DefaultGatewayRuntimeNodeConfig("runtime-test", t.TempDir(), "http://worker:8081/dispatch")
	node, err := config.BuildRuntimeNode(DispatchRuntimeConfig{
		Executor: &fakeExecutor{},
		Results:  DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})
	if err != nil {
		t.Fatalf("BuildRuntimeNode() error = %v", err)
	}
	if node.RemoteWorker != nil {
		t.Fatalf("remote worker = %#v, want nil", node.RemoteWorker)
	}
}

func TestWorkerRuntimeNodeExposesRemoteWorkerServer(t *testing.T) {
	config := DefaultWorkerRuntimeNodeConfig("runtime-test", t.TempDir())
	node, err := config.BuildRuntimeNode(DispatchRuntimeConfig{
		Executor: &fakeExecutor{},
		Results:  DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})
	if err != nil {
		t.Fatalf("BuildRuntimeNode() error = %v", err)
	}
	if node.RemoteWorker == nil || node.RemoteWorker.Server() == nil {
		t.Fatalf("remote worker = %#v", node.RemoteWorker)
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

func TestRuntimeNodeConfigBuildRuntimeNodeWiresCoreServices(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	node, err := config.BuildRuntimeNode(DispatchRuntimeConfig{
		Executor: &fakeExecutor{},
		Results:  DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})
	if err != nil {
		t.Fatalf("BuildRuntimeNode() error = %v", err)
	}
	if node == nil {
		t.Fatal("node is nil")
	}
	if node.Sandbox == nil {
		t.Fatal("node.Sandbox is nil")
	}
	if node.Dispatcher == nil {
		t.Fatal("node.Dispatcher is nil")
	}
	if node.State.Snapshots == nil || node.State.Events == nil || node.State.Threads == nil {
		t.Fatalf("state = %#v", node.State)
	}
	if node.RemoteWorker == nil || node.RemoteWorker.Server() == nil {
		t.Fatalf("remote worker = %#v", node.RemoteWorker)
	}
	if err := node.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRuntimeNodeConfigBuildRuntimeNodeWithoutDispatchBindingsLeavesTransportUnbound(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	node, err := config.BuildRuntimeNode(DispatchRuntimeConfig{})
	if err != nil {
		t.Fatalf("BuildRuntimeNode() error = %v", err)
	}
	if node == nil {
		t.Fatal("node is nil")
	}
	if node.Sandbox == nil {
		t.Fatal("node.Sandbox is nil")
	}
	if node.State.Snapshots == nil || node.State.Events == nil || node.State.Threads == nil {
		t.Fatalf("state = %#v", node.State)
	}
	if node.Dispatcher != nil {
		t.Fatalf("Dispatcher = %#v, want nil before BindDispatch", node.Dispatcher)
	}
	if node.RemoteWorker != nil {
		t.Fatalf("RemoteWorker = %#v, want nil before BindDispatch", node.RemoteWorker)
	}
}

func TestRuntimeNodeConfigBuildDispatchRuntimeSuppliesNodeDefaults(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	runtime := config.BuildDispatchRuntime(nil, nil)
	if runtime.Remote == nil {
		t.Fatal("runtime.Remote is nil")
	}
	if runtime.Results == nil {
		t.Fatal("runtime.Results is nil")
	}
}

func TestRuntimeNodeConfigBuildRuntimeNodeWithProvidersOverridesDefaultBuilders(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	customState := RuntimeStatePlane{
		Snapshots: NewInMemoryRunStore(),
		Events:    NewInMemoryRunEventStore(),
		Threads:   NewInMemoryThreadStateStore(),
	}
	executor := &fakeExecutor{}
	customSandbox := NewLocalSandboxManager("custom-runtime", t.TempDir())
	node, err := config.BuildRuntimeNodeWithProviders(DispatchRuntimeConfig{
		Codec: WorkerPlanCodec{},
	}, RuntimeNodeProviders{
		StatePlane: RuntimeStatePlaneFactoryFunc(func(RuntimeNodeConfig) RuntimeStatePlane {
			return customState
		}),
		Transport: WorkerTransportFactoryFunc(func(_ WorkerTransportConfig, runtime DispatchRuntimeConfig) WorkerTransport {
			return NewDirectWorkerTransportWithResults(executor, DispatchEnvelopeCodec{Plans: runtime.Codec}, nil)
		}),
		Sandbox: SandboxManagerFactoryFunc(func(SandboxManagerConfig) (*SandboxResourceManager, error) {
			return customSandbox, nil
		}),
	})
	if err != nil {
		t.Fatalf("BuildRuntimeNodeWithProviders() error = %v", err)
	}
	if node.State.Snapshots != customState.Snapshots || node.State.Events != customState.Events || node.State.Threads != customState.Threads {
		t.Fatalf("state = %#v", node.State)
	}
	if node.Sandbox != customSandbox {
		t.Fatalf("sandbox = %#v", node.Sandbox)
	}
	result, err := node.Dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{RunID: "run-providers", ThreadID: "thread-providers"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-providers" {
		t.Fatalf("executor req = %#v", executor.req)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-providers" {
		t.Fatalf("result = %#v", result)
	}
	if err := node.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRuntimeNodeConfigBuildStatePlaneWithProvidersOverridesDefaultStores(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	customSnapshots := NewInMemoryRunStore()
	customEvents := NewInMemoryRunEventStore()
	customThreads := NewInMemoryThreadStateStore()
	plane := config.BuildStatePlaneWithProviders(RuntimeStatePlaneProviders{
		Snapshots: RunSnapshotStoreFactoryFunc(func(RuntimeNodeConfig) RunSnapshotStore { return customSnapshots }),
		Events:    RunEventStoreFactoryFunc(func(RuntimeNodeConfig) RunEventStore { return customEvents }),
		Threads:   ThreadStateStoreFactoryFunc(func(RuntimeNodeConfig) ThreadStateStore { return customThreads }),
	})
	if plane.Snapshots != customSnapshots {
		t.Fatalf("Snapshots = %T", plane.Snapshots)
	}
	if plane.Events != customEvents {
		t.Fatalf("Events = %T", plane.Events)
	}
	if plane.Threads != customThreads {
		t.Fatalf("Threads = %T", plane.Threads)
	}
}

func TestRuntimeNodeConfigBuildDispatchRuntimeWithProvidersOverridesRemoteWiring(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	client := &fakeRemoteClient{}
	protocol := fakeRemoteProtocol{}
	runtime := config.BuildDispatchRuntimeWithProviders(nil, nil, RemoteWorkerProviders{
		Client: RemoteWorkerClientFactoryFunc(func(RuntimeNodeConfig) RemoteWorkerClient {
			return client
		}),
		Protocol: RemoteWorkerProtocolFactoryFunc(func(RuntimeNodeConfig, DispatchResultMarshaler) RemoteWorkerProtocol {
			return protocol
		}),
	})
	if runtime.Remote != client {
		t.Fatalf("runtime.Remote = %#v", runtime.Remote)
	}
	if runtime.Protocol == nil {
		t.Fatal("runtime.Protocol is nil")
	}
	if _, ok := runtime.Protocol.(fakeRemoteProtocol); !ok {
		t.Fatalf("runtime.Protocol = %T", runtime.Protocol)
	}
}

func TestRuntimeNodeConfigBuildRuntimeNodeWithProvidersOverridesRemoteServerFactory(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	node, err := config.BuildRuntimeNodeWithProviders(DispatchRuntimeConfig{
		Executor: &fakeExecutor{},
	}, RuntimeNodeProviders{
		StatePlane: DefaultRuntimeNodeProviders().StatePlane,
		Transport:  DefaultRuntimeNodeProviders().Transport,
		Sandbox:    DefaultRuntimeNodeProviders().Sandbox,
		Remote: RemoteWorkerProviders{
			Client:   DefaultRemoteWorkerProviders().Client,
			Protocol: DefaultRemoteWorkerProviders().Protocol,
			Server: RemoteWorkerHTTPServerFactoryFunc(func(_ RemoteWorkerServerConfig, _ WorkerTransport, _ RemoteWorkerProtocol) *http.Server {
				return &http.Server{Addr: "127.0.0.1:29081", Handler: http.NewServeMux()}
			}),
		},
	})
	if err != nil {
		t.Fatalf("BuildRuntimeNodeWithProviders() error = %v", err)
	}
	if node.RemoteWorker == nil || node.RemoteWorker.Server() == nil {
		t.Fatalf("remote worker = %#v", node.RemoteWorker)
	}
	if node.RemoteWorker.Server().Addr != "127.0.0.1:29081" {
		t.Fatalf("remote worker addr = %q", node.RemoteWorker.Server().Addr)
	}
}

func TestRuntimeNodeBindDispatchReusesProviders(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	executor := &fakeExecutor{}
	node, err := config.BuildRuntimeNodeWithProviders(DispatchRuntimeConfig{}, RuntimeNodeProviders{
		StatePlane: DefaultRuntimeNodeProviders().StatePlane,
		Transport: WorkerTransportFactoryFunc(func(_ WorkerTransportConfig, runtime DispatchRuntimeConfig) WorkerTransport {
			return NewDirectWorkerTransportWithResults(executor, DispatchEnvelopeCodec{Plans: runtime.Codec}, nil)
		}),
		Sandbox: DefaultRuntimeNodeProviders().Sandbox,
		Remote: RemoteWorkerProviders{
			Client:   DefaultRemoteWorkerProviders().Client,
			Protocol: DefaultRemoteWorkerProviders().Protocol,
			Server: RemoteWorkerHTTPServerFactoryFunc(func(_ RemoteWorkerServerConfig, _ WorkerTransport, _ RemoteWorkerProtocol) *http.Server {
				return &http.Server{Addr: "127.0.0.1:39081", Handler: http.NewServeMux()}
			}),
		},
	})
	if err != nil {
		t.Fatalf("BuildRuntimeNodeWithProviders() error = %v", err)
	}
	node.BindDispatch(DispatchRuntimeConfig{Codec: WorkerPlanCodec{}})
	result, err := node.Dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{RunID: "run-bind", ThreadID: "thread-bind"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-bind" {
		t.Fatalf("executor req = %#v", executor.req)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-bind" {
		t.Fatalf("result = %#v", result)
	}
	if node.RemoteWorker == nil || node.RemoteWorker.Server() == nil || node.RemoteWorker.Server().Addr != "127.0.0.1:39081" {
		t.Fatalf("remote worker = %#v", node.RemoteWorker)
	}
}

type fakeRemoteProtocol struct{}

func (fakeRemoteProtocol) EncodeRequest(env WorkerDispatchEnvelope) ([]byte, error) {
	return json.Marshal(RemoteWorkerRequest{Envelope: env})
}

func (fakeRemoteProtocol) DecodeRequest(data []byte) (WorkerDispatchEnvelope, error) {
	var req RemoteWorkerRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return WorkerDispatchEnvelope{}, err
	}
	return req.Envelope, nil
}

func (fakeRemoteProtocol) EncodeResponse(result *DispatchResult) ([]byte, error) {
	return json.Marshal(RemoteWorkerResponse{Result: []byte("ok")})
}

func (fakeRemoteProtocol) DecodeResponse(_ []byte) (*DispatchResult, error) {
	return &DispatchResult{}, nil
}
