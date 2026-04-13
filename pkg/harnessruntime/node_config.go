package harnessruntime

import (
	"net/http"
	"path/filepath"
	goruntime "runtime"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

// RuntimeNodeConfig describes the in-process runtime node shape. It keeps
// deployment-facing execution choices outside compat protocol code.
type RuntimeNodeConfig struct {
	Sandbox      SandboxManagerConfig
	Transport    WorkerTransportConfig
	State        RuntimeStateStoreConfig
	RemoteWorker RemoteWorkerServerConfig
}

type RuntimeStateStoreBackend string

const (
	RuntimeStateStoreBackendInMemory RuntimeStateStoreBackend = "in-memory"
	RuntimeStateStoreBackendFile     RuntimeStateStoreBackend = "file"
)

type RuntimeStateStoreConfig struct {
	Backend         RuntimeStateStoreBackend
	SnapshotBackend RuntimeStateStoreBackend
	EventBackend    RuntimeStateStoreBackend
	ThreadBackend   RuntimeStateStoreBackend
	Root            string
}

type RemoteWorkerServerConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
}

func DefaultRuntimeNodeConfig(name, root string) RuntimeNodeConfig {
	workers := goruntime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	return RuntimeNodeConfig{
		Sandbox: SandboxManagerConfig{
			Backend: SandboxBackendLocalLinux,
			Name:    name,
			Root:    root,
			Policy:  harness.FeatureSandboxPolicy{},
		},
		Transport: WorkerTransportConfig{
			Backend: WorkerTransportBackendQueue,
			Buffer:  defaultRunQueueBuffer,
			Workers: workers,
		},
		State: RuntimeStateStoreConfig{
			Backend:         RuntimeStateStoreBackendInMemory,
			SnapshotBackend: RuntimeStateStoreBackendInMemory,
			EventBackend:    RuntimeStateStoreBackendInMemory,
			ThreadBackend:   RuntimeStateStoreBackendInMemory,
		},
		RemoteWorker: RemoteWorkerServerConfig{
			Addr:              ":8081",
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

func (c RuntimeNodeConfig) BuildSandboxManager() (*SandboxResourceManager, error) {
	return NewSandboxManagerFromConfig(c.Sandbox)
}

func (c RuntimeNodeConfig) BuildDispatcher(runtime DispatchRuntimeConfig) RunDispatcher {
	return NewRuntimeDispatcher(DispatchConfig{
		Topology: transportTopology(c.Transport.Backend),
		Endpoint: c.Transport.Endpoint,
		Buffer:   c.Transport.Buffer,
		Workers:  c.Transport.Workers,
	}, runtime)
}

func (c RuntimeNodeConfig) BuildWorkerTransport(runtime DispatchRuntimeConfig) WorkerTransport {
	config := c.Transport
	if normalizeTransportBackend(config.Backend) == WorkerTransportBackendRemote {
		config.Backend = WorkerTransportBackendQueue
		config.Endpoint = ""
	}
	return buildWorkerTransport(config, runtime)
}

func (c RuntimeNodeConfig) BuildRemoteWorkerServer(runtime DispatchRuntimeConfig) *HTTPRemoteWorkerServer {
	return NewHTTPRemoteWorkerServer(c.BuildWorkerTransport(runtime), defaultRemoteWorkerProtocol(runtime.Protocol, runtime.Results))
}

func (c RuntimeNodeConfig) BuildRemoteWorkerHandler(runtime DispatchRuntimeConfig) http.Handler {
	server := c.BuildRemoteWorkerServer(runtime)
	return server.Handler()
}

func (c RuntimeNodeConfig) BuildRemoteWorkerHTTPServer(runtime DispatchRuntimeConfig) *http.Server {
	config := c.RemoteWorker
	addr := config.Addr
	if addr == "" {
		addr = ":8081"
	}
	timeout := config.ReadHeaderTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &http.Server{
		Addr:              addr,
		Handler:           c.BuildRemoteWorkerHandler(runtime),
		ReadHeaderTimeout: timeout,
	}
}

func (c RuntimeNodeConfig) BuildRemoteWorkerNode(runtime DispatchRuntimeConfig) *HTTPRemoteWorkerNode {
	return NewHTTPRemoteWorkerNode(c.BuildRemoteWorkerHTTPServer(runtime))
}

func (c RuntimeNodeConfig) BuildRunSnapshotStore() RunSnapshotStore {
	switch c.normalizedSnapshotBackend() {
	case RuntimeStateStoreBackendFile:
		return NewJSONFileRunStore(filepath.Join(c.State.Root, "runs"))
	default:
		return NewInMemoryRunStore()
	}
}

func (c RuntimeNodeConfig) BuildThreadStateStore() ThreadStateStore {
	switch c.normalizedThreadBackend() {
	case RuntimeStateStoreBackendFile:
		return NewJSONFileThreadStateStore(filepath.Join(c.State.Root, "threads"))
	default:
		return NewInMemoryThreadStateStore()
	}
}

func (c RuntimeNodeConfig) BuildRunEventStore() RunEventStore {
	switch c.normalizedEventBackend() {
	case RuntimeStateStoreBackendFile:
		return NewJSONFileRunEventStore(filepath.Join(c.State.Root, "events"))
	default:
		return NewInMemoryRunEventStore()
	}
}

func (c RuntimeNodeConfig) BuildStatePlane() RuntimeStatePlane {
	return RuntimeStatePlane{
		Snapshots: c.BuildRunSnapshotStore(),
		Events:    c.BuildRunEventStore(),
		Threads:   c.BuildThreadStateStore(),
	}
}

func transportTopology(backend WorkerTransportBackend) DispatchTopology {
	switch normalizeTransportBackend(backend) {
	case WorkerTransportBackendDirect:
		return DispatchTopologyDirect
	case WorkerTransportBackendRemote:
		return DispatchTopologyRemote
	default:
		return DispatchTopologyQueued
	}
}

func (c RuntimeNodeConfig) normalizedStateBackend(value RuntimeStateStoreBackend) RuntimeStateStoreBackend {
	switch value {
	case RuntimeStateStoreBackendFile:
		return RuntimeStateStoreBackendFile
	default:
		return RuntimeStateStoreBackendInMemory
	}
}

func (c RuntimeNodeConfig) normalizedSnapshotBackend() RuntimeStateStoreBackend {
	if c.State.SnapshotBackend != "" {
		return c.normalizedStateBackend(c.State.SnapshotBackend)
	}
	return c.normalizedStateBackend(c.State.Backend)
}

func (c RuntimeNodeConfig) normalizedThreadBackend() RuntimeStateStoreBackend {
	if c.State.ThreadBackend != "" {
		return c.normalizedStateBackend(c.State.ThreadBackend)
	}
	return c.normalizedStateBackend(c.State.Backend)
}

func (c RuntimeNodeConfig) normalizedEventBackend() RuntimeStateStoreBackend {
	if c.State.EventBackend != "" {
		return c.normalizedStateBackend(c.State.EventBackend)
	}
	return c.normalizedStateBackend(c.State.Backend)
}
