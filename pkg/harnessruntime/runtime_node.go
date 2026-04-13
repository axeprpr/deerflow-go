package harnessruntime

import (
	"context"
	"net"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type RuntimeStatePlaneFactory interface {
	Build(RuntimeNodeConfig) RuntimeStatePlane
}

type RuntimeStatePlaneFactoryFunc func(RuntimeNodeConfig) RuntimeStatePlane

func (f RuntimeStatePlaneFactoryFunc) Build(config RuntimeNodeConfig) RuntimeStatePlane {
	return f(config)
}

type WorkerTransportFactory interface {
	Build(WorkerTransportConfig, DispatchRuntimeConfig) WorkerTransport
}

type WorkerTransportFactoryFunc func(WorkerTransportConfig, DispatchRuntimeConfig) WorkerTransport

func (f WorkerTransportFactoryFunc) Build(config WorkerTransportConfig, runtime DispatchRuntimeConfig) WorkerTransport {
	return f(config, runtime)
}

type SandboxManagerBuilder interface {
	Build(SandboxManagerConfig) (*SandboxResourceManager, error)
}

type SandboxManagerFactoryFunc func(SandboxManagerConfig) (*SandboxResourceManager, error)

func (f SandboxManagerFactoryFunc) Build(config SandboxManagerConfig) (*SandboxResourceManager, error) {
	return f(config)
}

type RuntimeNodeProviders struct {
	StatePlane RuntimeStatePlaneFactory
	Transport  WorkerTransportFactory
	Sandbox    SandboxManagerBuilder
	Remote     RemoteWorkerProviders
}

func DefaultRuntimeNodeProviders() RuntimeNodeProviders {
	return RuntimeNodeProviders{
		StatePlane: RuntimeStatePlaneFactoryFunc(func(config RuntimeNodeConfig) RuntimeStatePlane {
			return config.BuildStatePlaneWithProviders(DefaultRuntimeStatePlaneProviders())
		}),
		Transport: WorkerTransportFactoryFunc(func(config WorkerTransportConfig, runtime DispatchRuntimeConfig) WorkerTransport {
			return buildWorkerTransport(config, runtime)
		}),
		Sandbox: DefaultSandboxManagerFactory(),
		Remote:  DefaultRemoteWorkerProviders(),
	}
}

type RuntimeNode struct {
	Config       RuntimeNodeConfig
	Providers    RuntimeNodeProviders
	State        RuntimeStatePlane
	Dispatcher   RunDispatcher
	Sandbox      *SandboxResourceManager
	RemoteWorker *HTTPRemoteWorkerNode
	Memory       *MemoryService
	Tools        harness.ToolRuntime
}

func (c RuntimeNodeConfig) BuildRuntimeNode(runtime DispatchRuntimeConfig) (*RuntimeNode, error) {
	return c.BuildRuntimeNodeWithProviders(runtime, DefaultRuntimeNodeProviders())
}

func (c RuntimeNodeConfig) BuildRuntimeNodeWithProviders(runtime DispatchRuntimeConfig, providers RuntimeNodeProviders) (*RuntimeNode, error) {
	if providers.StatePlane == nil {
		providers.StatePlane = DefaultRuntimeNodeProviders().StatePlane
	}
	if providers.Transport == nil {
		providers.Transport = DefaultRuntimeNodeProviders().Transport
	}
	if providers.Sandbox == nil {
		providers.Sandbox = DefaultRuntimeNodeProviders().Sandbox
	}
	if providers.Remote.Client == nil {
		providers.Remote.Client = DefaultRemoteWorkerProviders().Client
	}
	if providers.Remote.Protocol == nil {
		providers.Remote.Protocol = DefaultRemoteWorkerProviders().Protocol
	}
	if providers.Remote.Server == nil {
		providers.Remote.Server = DefaultRemoteWorkerProviders().Server
	}
	sandboxManager, err := providers.Sandbox.Build(c.Sandbox)
	if err != nil {
		return nil, err
	}
	node := &RuntimeNode{
		Config:    c,
		Providers: providers,
		State:     providers.StatePlane.Build(c),
		Sandbox:   sandboxManager,
	}
	if runtime.hasBindings() {
		node.BindDispatch(runtime)
	}
	return node, nil
}

func (n *RuntimeNode) SnapshotStore() RunSnapshotStore {
	if n == nil {
		return nil
	}
	return n.State.Snapshots
}

func (n *RuntimeNode) EventStore() RunEventStore {
	if n == nil {
		return nil
	}
	return n.State.Events
}

func (n *RuntimeNode) ThreadStateStore() ThreadStateStore {
	if n == nil {
		return nil
	}
	return n.State.Threads
}

func (n *RuntimeNode) MemoryService() *MemoryService {
	if n == nil {
		return nil
	}
	return n.Memory
}

func (n *RuntimeNode) MemoryRuntime() *harness.MemoryRuntime {
	if n == nil || n.Memory == nil {
		return nil
	}
	return n.Memory.Runtime()
}

func (n *RuntimeNode) RunDispatcher() RunDispatcher {
	if n == nil {
		return nil
	}
	return n.Dispatcher
}

func (n *RuntimeNode) ToolRuntime() harness.ToolRuntime {
	if n == nil {
		return nil
	}
	return n.Tools
}

func (n *RuntimeNode) SandboxManager() *SandboxResourceManager {
	if n == nil {
		return nil
	}
	return n.Sandbox
}

func (n *RuntimeNode) ConfiguredSandboxRuntime() harness.SandboxRuntime {
	if n == nil {
		return nil
	}
	return n.SandboxRuntime(n.Config.Sandbox.Policy)
}

func (n *RuntimeNode) ConfiguredSandboxProvider() harness.SandboxProvider {
	runtime := n.ConfiguredSandboxRuntime()
	if runtime == nil {
		return nil
	}
	return runtime.Provider()
}

func (n *RuntimeNode) SandboxRuntime(policy harness.SandboxPolicy) harness.SandboxRuntime {
	if n == nil || n.Sandbox == nil {
		return nil
	}
	if policy == nil {
		policy = harness.FeatureSandboxPolicy{}
	}
	return n.Sandbox.Runtime(policy)
}

func (n *RuntimeNode) RemoteWorkerAddr() string {
	if n == nil || n.RemoteWorker == nil {
		return ""
	}
	return n.RemoteWorker.Addr()
}

func (n *RuntimeNode) Start() error {
	if n == nil {
		return nil
	}
	return n.StartRemoteWorker()
}

func (n *RuntimeNode) StartRemoteWorker() error {
	if n == nil || n.RemoteWorker == nil {
		return nil
	}
	return n.RemoteWorker.Start()
}

func (n *RuntimeNode) Serve(listener net.Listener) error {
	if n == nil {
		return nil
	}
	return n.ServeRemoteWorker(listener)
}

func (n *RuntimeNode) ServeRemoteWorker(listener net.Listener) error {
	if n == nil || n.RemoteWorker == nil {
		return nil
	}
	return n.RemoteWorker.Serve(listener)
}

func (n *RuntimeNode) BindDispatch(runtime DispatchRuntimeConfig) {
	if n == nil {
		return
	}
	providers := n.Providers
	if providers.Transport == nil {
		providers.Transport = DefaultRuntimeNodeProviders().Transport
	}
	if providers.Remote.Client == nil {
		providers.Remote.Client = DefaultRemoteWorkerProviders().Client
	}
	if providers.Remote.Protocol == nil {
		providers.Remote.Protocol = DefaultRemoteWorkerProviders().Protocol
	}
	if providers.Remote.Server == nil {
		providers.Remote.Server = DefaultRemoteWorkerProviders().Server
	}
	transport := providers.Transport.Build(n.Config.Transport, runtime)
	protocol := runtime.Protocol
	if protocol == nil {
		protocol = providers.Remote.Protocol.Build(n.Config, runtime.Results)
	}
	n.Dispatcher = transportRunDispatcher{transport: transport, codec: DispatchEnvelopeCodec{Plans: runtime.Codec}}
	n.RemoteWorker = NewHTTPRemoteWorkerNode(n.Config.BuildRemoteWorkerHTTPServerWithProviders(transport, protocol, providers.Remote))
}

func (n *RuntimeNode) BindDispatchSource(source func() *harness.Runtime, specs WorkerSpecRuntime) {
	if n == nil {
		return
	}
	n.BindDispatch(n.Config.BuildDispatchRuntimeWithProviders(source, specs, n.Providers.Remote))
}

func (n *RuntimeNode) BindMemoryService(service *MemoryService) {
	if n == nil {
		return
	}
	n.Memory = service
}

func (n *RuntimeNode) BindToolRuntime(runtime harness.ToolRuntime) {
	if n == nil {
		return
	}
	n.Tools = runtime
}

func (c DispatchRuntimeConfig) hasBindings() bool {
	return c.Executor != nil ||
		c.Runtime != nil ||
		c.Specs != nil ||
		c.Codec != nil ||
		c.Results != nil ||
		c.Transport != nil ||
		c.Remote != nil ||
		c.Protocol != nil
}

func (n *RuntimeNode) Close(ctx context.Context) error {
	if n == nil {
		return nil
	}
	var closeErr error
	if closer, ok := n.Dispatcher.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if n.RemoteWorker != nil {
		if err := n.RemoteWorker.Shutdown(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if n.Memory != nil {
		if err := n.Memory.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if n.Sandbox != nil {
		if err := n.Sandbox.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}
