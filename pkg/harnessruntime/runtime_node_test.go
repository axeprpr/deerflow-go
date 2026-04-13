package harnessruntime

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRuntimeNodeRemoteWorkerAddr(t *testing.T) {
	node := &RuntimeNode{RemoteWorker: NewHTTPRemoteWorkerNode(buildTestHTTPServer("127.0.0.1:49081"))}
	if got := node.RemoteWorkerAddr(); got != "127.0.0.1:49081" {
		t.Fatalf("RemoteWorkerAddr() = %q", got)
	}
}

func TestRuntimeNodeLaunchSpec(t *testing.T) {
	node := &RuntimeNode{
		Config:       RuntimeNodeConfig{Role: RuntimeNodeRoleWorker},
		RemoteWorker: NewHTTPRemoteWorkerNode(buildTestHTTPServer("127.0.0.1:49081")),
	}
	spec := node.LaunchSpec()
	if spec.Role != RuntimeNodeRoleWorker {
		t.Fatalf("Role = %q, want %q", spec.Role, RuntimeNodeRoleWorker)
	}
	if !spec.ServesRemoteWorker {
		t.Fatal("ServesRemoteWorker = false, want true")
	}
	if spec.RemoteWorkerAddr != "127.0.0.1:49081" {
		t.Fatalf("RemoteWorkerAddr = %q", spec.RemoteWorkerAddr)
	}
}

func TestRuntimeNodeLauncherExposesRemoteWorkerHandler(t *testing.T) {
	node := &RuntimeNode{
		Config:       RuntimeNodeConfig{Role: RuntimeNodeRoleWorker},
		RemoteWorker: NewHTTPRemoteWorkerNode(buildTestHTTPServer("127.0.0.1:49081")),
	}
	node.RemoteWorker.Server().Handler = http.NewServeMux()
	launcher := NewRuntimeNodeLauncher(node)
	if launcher.Node() != node {
		t.Fatalf("Node() = %#v want %#v", launcher.Node(), node)
	}
	if launcher.Spec().Role != RuntimeNodeRoleWorker {
		t.Fatalf("Spec().Role = %q", launcher.Spec().Role)
	}
	if launcher.Handler() == nil {
		t.Fatal("Handler() = nil")
	}
}

func TestRuntimeNodeStartDelegatesToRemoteWorker(t *testing.T) {
	node := &RuntimeNode{}
	if err := node.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestRuntimeNodeServeRemoteWorkerBridgesDispatch(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	executor := &fakeExecutor{}
	node, err := config.BuildRuntimeNodeWithProviders(DispatchRuntimeConfig{Codec: WorkerPlanCodec{}}, RuntimeNodeProviders{
		StatePlane: DefaultRuntimeNodeProviders().StatePlane,
		Transport: WorkerTransportFactoryFunc(func(_ WorkerTransportConfig, runtime DispatchRuntimeConfig) WorkerTransport {
			return NewDirectWorkerTransportWithResults(executor, DispatchEnvelopeCodec{Plans: runtime.Codec}, nil)
		}),
		Sandbox: DefaultRuntimeNodeProviders().Sandbox,
		Remote:  DefaultRuntimeNodeProviders().Remote,
	})
	if err != nil {
		t.Fatalf("BuildRuntimeNodeWithProviders() error = %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- node.Serve(listener)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = node.Close(shutdownCtx)
		select {
		case serveErr := <-errCh:
			if serveErr != nil && serveErr != http.ErrServerClosed {
				t.Fatalf("ServeRemoteWorker() error = %v", serveErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("ServeRemoteWorker() did not exit")
		}
	}()

	dispatcher := NewRuntimeDispatcher(DispatchConfig{
		Topology: DispatchTopologyRemote,
		Endpoint: "http://" + listener.Addr().String() + DefaultRemoteWorkerDispatchPath,
	}, DispatchRuntimeConfig{
		Remote:  NewHTTPRemoteWorkerClient(nil),
		Results: DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
	})

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{RunID: "run-node", ThreadID: "thread-node"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-node" {
		t.Fatalf("executor req = %#v", executor.req)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-node" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRuntimeNodeBindDispatchSourceUsesNodeProviders(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	executor := &fakeExecutor{}
	node, err := config.BuildRuntimeNodeWithProviders(DispatchRuntimeConfig{}, RuntimeNodeProviders{
		StatePlane: DefaultRuntimeNodeProviders().StatePlane,
		Transport: WorkerTransportFactoryFunc(func(_ WorkerTransportConfig, runtime DispatchRuntimeConfig) WorkerTransport {
			return NewDirectWorkerTransportWithResults(executor, DispatchEnvelopeCodec{Plans: runtime.Codec}, runtime.Results)
		}),
		Sandbox: DefaultRuntimeNodeProviders().Sandbox,
		Remote:  DefaultRuntimeNodeProviders().Remote,
	})
	if err != nil {
		t.Fatalf("BuildRuntimeNodeWithProviders() error = %v", err)
	}
	node.BindDispatchSource(nil, nil)
	result, err := node.Dispatcher.Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{RunID: "run-bind-source", ThreadID: "thread-bind-source"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !executor.called || executor.req.Plan.RunID != "run-bind-source" {
		t.Fatalf("executor req = %#v", executor.req)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-bind-source" {
		t.Fatalf("result = %#v", result)
	}
}

func buildTestHTTPServer(addr string) *http.Server {
	return &http.Server{Addr: addr}
}
