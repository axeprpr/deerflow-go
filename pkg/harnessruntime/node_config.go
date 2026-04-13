package harnessruntime

import (
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

// RuntimeNodeConfig describes the in-process runtime node shape. It keeps
// deployment-facing execution choices outside compat protocol code.
type RuntimeNodeConfig struct {
	Role         RuntimeNodeRole
	Sandbox      SandboxManagerConfig
	Transport    WorkerTransportConfig
	State        RuntimeStateStoreConfig
	Memory       RuntimeMemoryConfig
	RemoteWorker RemoteWorkerServerConfig
}

type RuntimeNodeRole string

const (
	RuntimeNodeRoleAllInOne RuntimeNodeRole = "all-in-one"
	RuntimeNodeRoleGateway  RuntimeNodeRole = "gateway"
	RuntimeNodeRoleWorker   RuntimeNodeRole = "worker"
)

type RuntimeStateStoreBackend string

const (
	RuntimeStateStoreBackendInMemory RuntimeStateStoreBackend = "in-memory"
	RuntimeStateStoreBackendFile     RuntimeStateStoreBackend = "file"
	RuntimeStateStoreBackendSQLite   RuntimeStateStoreBackend = "sqlite"
	RuntimeStateStoreBackendRemote   RuntimeStateStoreBackend = "remote"
)

type RuntimeStateStoreConfig struct {
	Provider        RuntimeStateProviderMode
	Backend         RuntimeStateStoreBackend
	SnapshotBackend RuntimeStateStoreBackend
	EventBackend    RuntimeStateStoreBackend
	ThreadBackend   RuntimeStateStoreBackend
	Root            string
	URL             string
	SnapshotURL     string
	EventURL        string
	ThreadURL       string
}

type RemoteWorkerServerConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
}

type RuntimeMemoryConfig struct {
	StoreURL string
}

type RuntimeStateProviderMode string

const (
	RuntimeStateProviderModeAuto         RuntimeStateProviderMode = "auto"
	RuntimeStateProviderModeIsolated     RuntimeStateProviderMode = "isolated"
	RuntimeStateProviderModeSharedSQLite RuntimeStateProviderMode = "shared-sqlite"
)

func DefaultRuntimeNodeConfig(name, root string) RuntimeNodeConfig {
	workers := goruntime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	return RuntimeNodeConfig{
		Role: RuntimeNodeRoleAllInOne,
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

func DefaultGatewayRuntimeNodeConfig(name, root, endpoint string) RuntimeNodeConfig {
	config := DefaultRuntimeNodeConfig(name, root)
	config.Role = RuntimeNodeRoleGateway
	config.Transport.Backend = WorkerTransportBackendRemote
	config.Transport.Endpoint = strings.TrimSpace(endpoint)
	return config
}

func DefaultWorkerRuntimeNodeConfig(name, root string) RuntimeNodeConfig {
	config := DefaultRuntimeNodeConfig(name, root)
	config.Role = RuntimeNodeRoleWorker
	config.Transport.Backend = WorkerTransportBackendQueue
	config.Transport.Endpoint = ""
	return config
}

func ResolveRuntimeNodeConfig(existing RuntimeNodeConfig, name, root string) RuntimeNodeConfig {
	if existing.Transport.Backend != "" {
		return existing
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = filepath.Join(os.TempDir(), "deerflow-langgraph-sandbox")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "langgraph"
	}
	return DefaultRuntimeNodeConfig(name, root)
}

func (c RuntimeNodeConfig) servesRemoteWorker() bool {
	switch c.Role {
	case RuntimeNodeRoleGateway:
		return false
	default:
		return true
	}
}

func (c RuntimeNodeConfig) servesRemoteSandbox() bool {
	if !c.servesRemoteWorker() {
		return false
	}
	return c.Sandbox.Normalized().Backend == SandboxBackendLocalLinux
}

func (c RuntimeNodeConfig) servesRemoteState() bool {
	if !c.servesRemoteWorker() {
		return false
	}
	return c.normalizedSnapshotBackend() != RuntimeStateStoreBackendRemote ||
		c.normalizedEventBackend() != RuntimeStateStoreBackendRemote ||
		c.normalizedThreadBackend() != RuntimeStateStoreBackendRemote
}

func (c RuntimeNodeConfig) BuildSandboxManager() (*SandboxResourceManager, error) {
	return NewSandboxManagerFromConfig(c.Sandbox)
}

func (c RuntimeNodeConfig) BuildDispatchRuntime(source func() *harness.Runtime, specs WorkerSpecRuntime) DispatchRuntimeConfig {
	return c.BuildDispatchRuntimeWithProviders(source, specs, DefaultRemoteWorkerProviders())
}

func (c RuntimeNodeConfig) BuildDispatchRuntimeWithProviders(source func() *harness.Runtime, specs WorkerSpecRuntime, providers RemoteWorkerProviders) DispatchRuntimeConfig {
	if providers.Client == nil {
		providers.Client = DefaultRemoteWorkerProviders().Client
	}
	if providers.Protocol == nil {
		providers.Protocol = DefaultRemoteWorkerProviders().Protocol
	}
	return DispatchRuntimeConfig{
		Runtime:  source,
		Specs:    specs,
		Results:  DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()},
		Remote:   providers.Client.Build(c),
		Protocol: providers.Protocol.Build(c, DispatchResultCodec{Handles: NewInMemoryExecutionHandleRegistry()}),
	}
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
	return c.BuildRemoteWorkerHTTPServerWithProviders(c.BuildWorkerTransport(runtime), defaultRemoteWorkerProtocol(runtime.Protocol, runtime.Results), DefaultRemoteWorkerProviders())
}

func (c RuntimeNodeConfig) BuildRemoteWorkerHTTPServerWithTransport(transport WorkerTransport, protocol RemoteWorkerProtocol) *http.Server {
	return c.BuildRemoteWorkerHTTPServerWithProviders(transport, protocol, DefaultRemoteWorkerProviders())
}

func (c RuntimeNodeConfig) BuildRemoteWorkerHTTPServerWithProviders(transport WorkerTransport, protocol RemoteWorkerProtocol, providers RemoteWorkerProviders) *http.Server {
	if providers.Server == nil {
		providers.Server = DefaultRemoteWorkerProviders().Server
	}
	return providers.Server.Build(c.RemoteWorker, transport, protocol)
}

func (c RuntimeNodeConfig) BuildRemoteStateHTTPServer(state RuntimeStatePlane) *http.Server {
	return c.BuildRemoteStateHTTPServerWithProviders(state, DefaultRemoteStateProviders())
}

func (c RuntimeNodeConfig) BuildRemoteStateHTTPServerWithProviders(state RuntimeStatePlane, providers RemoteStateProviders) *http.Server {
	if providers.Protocol == nil {
		providers.Protocol = DefaultRemoteStateProviders().Protocol
	}
	if providers.Server == nil {
		providers.Server = DefaultRemoteStateProviders().Server
	}
	stateServer := providers.Server.Build(state, providers.Protocol.Build(c))
	return &http.Server{
		Addr:              c.RemoteWorker.Addr,
		ReadHeaderTimeout: c.RemoteWorker.ReadHeaderTimeout,
		Handler:           stateServer.Handler(),
	}
}

func (c RuntimeNodeConfig) BuildRemoteSandboxHTTPServer() *http.Server {
	return c.BuildRemoteSandboxHTTPServerWithProviders(DefaultRemoteSandboxProviders())
}

func (c RuntimeNodeConfig) BuildRemoteSandboxHTTPServerWithProviders(providers RemoteSandboxProviders) *http.Server {
	if providers.Protocol == nil {
		providers.Protocol = DefaultRemoteSandboxProviders().Protocol
	}
	if providers.Server == nil {
		providers.Server = DefaultRemoteSandboxProviders().Server
	}
	sandboxServer := providers.Server.Build(c.Sandbox, providers.Protocol.Build(c))
	return &http.Server{
		Addr:              c.RemoteWorker.Addr,
		ReadHeaderTimeout: c.RemoteWorker.ReadHeaderTimeout,
		Handler:           sandboxServer.Handler(),
	}
}

func (c RuntimeNodeConfig) BuildRemoteWorkerNode(runtime DispatchRuntimeConfig) *HTTPRemoteWorkerNode {
	return NewHTTPRemoteWorkerNode(c.BuildRemoteWorkerHTTPServer(runtime))
}

func (c RuntimeNodeConfig) BuildRuntimeStateLauncher() (*RuntimeStateLauncher, error) {
	return c.BuildRuntimeStateLauncherWithProviders(DefaultRuntimeNodeProviders())
}

func (c RuntimeNodeConfig) BuildRuntimeStateLauncherWithProviders(providers RuntimeNodeProviders) (*RuntimeStateLauncher, error) {
	providers = normalizeRuntimeNodeProviders(c, providers)
	state := providers.StatePlane.Build(c)
	server := c.BuildRemoteStateHTTPServerWithProviders(state, providers.RemoteState)
	return NewRuntimeStateLauncher(NewHTTPRemoteStateNode(server), state), nil
}

func (c RuntimeNodeConfig) BuildRuntimeSandboxLauncher() (*RuntimeSandboxLauncher, error) {
	return c.BuildRuntimeSandboxLauncherWithProviders(DefaultRuntimeNodeProviders())
}

func (c RuntimeNodeConfig) BuildRuntimeSandboxLauncherWithProviders(providers RuntimeNodeProviders) (*RuntimeSandboxLauncher, error) {
	providers = normalizeRuntimeNodeProviders(c, providers)
	server := c.BuildRemoteSandboxHTTPServerWithProviders(providers.RemoteSB)
	return NewRuntimeSandboxLauncher(NewHTTPRemoteSandboxNode(server)), nil
}

func (c RuntimeNodeConfig) BuildRunSnapshotStore() RunSnapshotStore {
	return DefaultRuntimeStatePlaneProviders().buildSnapshotStore(c)
}

func (c RuntimeNodeConfig) BuildThreadStateStore() ThreadStateStore {
	return DefaultRuntimeStatePlaneProviders().buildThreadStateStore(c)
}

func (c RuntimeNodeConfig) BuildRunEventStore() RunEventStore {
	return DefaultRuntimeStatePlaneProviders().buildEventStore(c)
}

func (c RuntimeNodeConfig) BuildStatePlane() RuntimeStatePlane {
	return c.BuildStatePlaneWithProviders(DefaultRuntimeStatePlaneProviders())
}

func (c RuntimeNodeConfig) BuildStatePlaneWithProviders(providers RuntimeStatePlaneProviders) RuntimeStatePlane {
	return providers.buildPlane(c)
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
	case RuntimeStateStoreBackendSQLite:
		return RuntimeStateStoreBackendSQLite
	case RuntimeStateStoreBackendRemote:
		return RuntimeStateStoreBackendRemote
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
