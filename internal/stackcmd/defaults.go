package stackcmd

import (
	"strings"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/internal/sandboxcmd"
	"github.com/axeprpr/deerflow-go/internal/statecmd"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func defaultSplitComponents() (langgraphcmd.Config, runtimecmd.NodeConfig, statecmd.Config, sandboxcmd.Config) {
	gateway := langgraphcmd.DefaultConfig()
	worker := runtimecmd.DefaultSplitWorkerNodeConfig()
	state := statecmd.DefaultConfig()
	sb := sandboxcmd.DefaultConfig()

	sharedDataRoot := firstNonEmpty(strings.TrimSpace(gateway.Runtime.DataRoot), strings.TrimSpace(worker.DataRoot))
	if sharedDataRoot != "" {
		gateway.Runtime.DataRoot = sharedDataRoot
		worker.DataRoot = sharedDataRoot
		state.Runtime.DataRoot = sharedDataRoot
		sb.Runtime.DataRoot = sharedDataRoot
	}
	worker.Provider = gateway.Provider
	worker.MaxTurns = gateway.Runtime.MaxTurns
	gateway.Runtime = runtimecmd.DefaultSplitGatewayNodeConfig(workerDispatchEndpoint(worker.Addr))
	worker.StateBackend = gateway.Runtime.StateBackend
	worker.SnapshotBackend = gateway.Runtime.SnapshotBackend
	worker.EventBackend = gateway.Runtime.EventBackend
	worker.ThreadBackend = gateway.Runtime.ThreadBackend
	worker.StateRoot = gateway.Runtime.StateRoot
	worker.StateStoreURL = gateway.Runtime.StateStoreURL
	worker.SnapshotStoreURL = gateway.Runtime.SnapshotStoreURL
	worker.EventStoreURL = gateway.Runtime.EventStoreURL
	worker.ThreadStoreURL = gateway.Runtime.ThreadStoreURL
	worker.MemoryStoreURL = gateway.Runtime.MemoryStoreURL
	worker.StateProvider = gateway.Runtime.StateProvider
	gateway.Runtime.Addr = worker.Addr
	gateway.Runtime.Endpoint = workerDispatchEndpoint(worker.Addr)
	gateway.Runtime.Provider = worker.Provider

	return gateway, worker, state, sb
}

func (c Config) applySplitIdentityDefaults() Config {
	if c.usesDedicatedStateService() {
		c.State.Runtime.Addr = deriveDedicatedServiceAddr(c.Worker.Addr, c.State.Runtime.Addr, defaultStateServiceAddr, 1)
		c.Sandbox.Runtime.Addr = deriveDedicatedServiceAddr(c.Worker.Addr, c.Sandbox.Runtime.Addr, defaultSandboxServiceAddr, 2)
	}
	if strings.TrimSpace(c.Worker.Provider) == "" {
		c.Worker.Provider = c.Gateway.Provider
	}
	if c.Worker.MaxTurns <= 0 {
		c.Worker.MaxTurns = c.Gateway.Runtime.MaxTurns
	}
	if strings.TrimSpace(c.Worker.DataRoot) == "" {
		c.Worker.DataRoot = c.Gateway.Runtime.DataRoot
	}
	if strings.TrimSpace(c.State.Runtime.DataRoot) == "" {
		c.State.Runtime.DataRoot = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.DataRoot), strings.TrimSpace(c.Worker.DataRoot))
	}
	if strings.TrimSpace(c.Sandbox.Runtime.DataRoot) == "" {
		c.Sandbox.Runtime.DataRoot = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.DataRoot), strings.TrimSpace(c.Worker.DataRoot))
	}
	if strings.TrimSpace(c.Worker.MemoryStoreURL) == "" {
		c.Worker.MemoryStoreURL = c.Gateway.Runtime.MemoryStoreURL
	}
	return c
}

func (c Config) applySplitStoreDefaults() Config {
	profile := c.Profile()
	if c.Worker.StateProvider == "" {
		c.Worker.StateProvider = c.Gateway.Runtime.StateProvider
	}
	if strings.TrimSpace(c.Worker.StateRoot) == "" {
		c.Worker.StateRoot = c.Gateway.Runtime.StateRoot
	}
	if strings.TrimSpace(c.Worker.StateStoreURL) == "" && !gatewayUsesRemoteState(c.Gateway.Runtime) {
		c.Worker.StateStoreURL = c.Gateway.Runtime.StateStoreURL
	}
	if strings.TrimSpace(c.Worker.SnapshotStoreURL) == "" && !gatewayUsesRemoteState(c.Gateway.Runtime) {
		c.Worker.SnapshotStoreURL = c.Gateway.Runtime.SnapshotStoreURL
	}
	if strings.TrimSpace(c.Worker.EventStoreURL) == "" && !gatewayUsesRemoteState(c.Gateway.Runtime) {
		c.Worker.EventStoreURL = c.Gateway.Runtime.EventStoreURL
	}
	if strings.TrimSpace(c.Worker.ThreadStoreURL) == "" && !gatewayUsesRemoteState(c.Gateway.Runtime) {
		c.Worker.ThreadStoreURL = c.Gateway.Runtime.ThreadStoreURL
	}
	c.Worker.TransportBackend = firstNonEmptyTransport(c.Worker.TransportBackend, profile.WorkerTransport)
	if !gatewayUsesRemoteState(c.Gateway.Runtime) {
		c.Worker.StateBackend = firstNonEmptyState(c.Worker.StateBackend, c.Gateway.Runtime.StateBackend)
		c.Worker.SnapshotBackend = firstNonEmptyState(c.Worker.SnapshotBackend, c.Gateway.Runtime.SnapshotBackend)
		c.Worker.EventBackend = firstNonEmptyState(c.Worker.EventBackend, c.Gateway.Runtime.EventBackend)
		c.Worker.ThreadBackend = firstNonEmptyState(c.Worker.ThreadBackend, c.Gateway.Runtime.ThreadBackend)
	}
	return c
}

func (c Config) applySplitTopologyDefaults() Config {
	c.Worker.Role = harnessruntime.RuntimeNodeRoleWorker
	c.Gateway.Runtime.Role = harnessruntime.RuntimeNodeRoleGateway
	c.Gateway.Runtime.Addr = c.Worker.Addr
	c.Gateway.Runtime.Provider = c.Worker.Provider
	c.Gateway.Runtime.MaxTurns = c.Worker.MaxTurns
	c.Gateway.Runtime.DataRoot = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.DataRoot), strings.TrimSpace(c.Worker.DataRoot))
	c.Gateway.Runtime.MemoryStoreURL = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.MemoryStoreURL), strings.TrimSpace(c.Worker.MemoryStoreURL))
	if c.Gateway.Runtime.StateProvider == "" {
		c.Gateway.Runtime.StateProvider = c.Worker.StateProvider
	}
	return c
}

func (c Config) applyDedicatedRemoteServices(prevGatewayEndpoint, prevGatewayStateStore string) Config {
	if c.usesDedicatedStateService() {
		stateURL := stateServiceEndpoint(c.State.Runtime.Addr)
		c.State = c.State.WithDefaults()
		c.Sandbox = c.Sandbox.WithDefaults()
		c.Gateway.Runtime.StateProvider = harnessruntime.RuntimeStateProviderModeAuto
		c.Gateway.Runtime.StateBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Gateway.Runtime.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Gateway.Runtime.EventBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Gateway.Runtime.ThreadBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Gateway.Runtime.StateStoreURL = stateURL
		c.Gateway.Runtime.SnapshotStoreURL = ""
		c.Gateway.Runtime.EventStoreURL = ""
		c.Gateway.Runtime.ThreadStoreURL = ""
		c.Gateway.Runtime.StateRoot = ""

		c.Worker.StateProvider = harnessruntime.RuntimeStateProviderModeAuto
		c.Worker.StateBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Worker.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Worker.EventBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Worker.ThreadBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Worker.StateStoreURL = stateURL
		c.Worker.SnapshotStoreURL = ""
		c.Worker.EventStoreURL = ""
		c.Worker.ThreadStoreURL = ""
		c.Worker.StateRoot = ""
		c.Worker.SandboxBackend = harnessruntime.SandboxBackendRemote
		c.Worker.SandboxEndpoint = sandboxServiceEndpoint(c.Sandbox.Runtime.Addr)
		return c
	}

	if gatewayUsesRemoteState(c.Gateway.Runtime) {
		c.Gateway.Runtime.StateBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.Gateway.Runtime.SnapshotBackend = firstNonEmptyState(c.Gateway.Runtime.SnapshotBackend, harnessruntime.RuntimeStateStoreBackendRemote)
		c.Gateway.Runtime.EventBackend = firstNonEmptyState(c.Gateway.Runtime.EventBackend, harnessruntime.RuntimeStateStoreBackendRemote)
		c.Gateway.Runtime.ThreadBackend = firstNonEmptyState(c.Gateway.Runtime.ThreadBackend, harnessruntime.RuntimeStateStoreBackendRemote)
		if prevGatewayStateStore == "" || prevGatewayStateStore == stateStoreEndpointFromWorkerDispatch(prevGatewayEndpoint) {
			c.Gateway.Runtime.StateStoreURL = workerStateEndpoint(c.Worker.Addr)
		} else {
			c.Gateway.Runtime.StateStoreURL = prevGatewayStateStore
		}
		return c
	}

	c.Gateway.Runtime.StateRoot = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.StateRoot), strings.TrimSpace(c.Worker.StateRoot))
	c.Gateway.Runtime.StateStoreURL = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.StateStoreURL), strings.TrimSpace(c.Worker.StateStoreURL))
	c.Gateway.Runtime.SnapshotStoreURL = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.SnapshotStoreURL), strings.TrimSpace(c.Worker.SnapshotStoreURL))
	c.Gateway.Runtime.EventStoreURL = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.EventStoreURL), strings.TrimSpace(c.Worker.EventStoreURL))
	c.Gateway.Runtime.ThreadStoreURL = firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.ThreadStoreURL), strings.TrimSpace(c.Worker.ThreadStoreURL))
	c.Gateway.Runtime.StateBackend = firstNonEmptyState(c.Gateway.Runtime.StateBackend, c.Worker.StateBackend)
	c.Gateway.Runtime.SnapshotBackend = firstNonEmptyState(c.Gateway.Runtime.SnapshotBackend, c.Worker.SnapshotBackend)
	c.Gateway.Runtime.EventBackend = firstNonEmptyState(c.Gateway.Runtime.EventBackend, c.Worker.EventBackend)
	c.Gateway.Runtime.ThreadBackend = firstNonEmptyState(c.Gateway.Runtime.ThreadBackend, c.Worker.ThreadBackend)
	return c
}

func (c Config) applyStateProviderDefaults() Config {
	c.Gateway.Runtime.Endpoint = workerDispatchEndpoint(c.Worker.Addr)
	profile := c.Profile()
	if c.usesDedicatedStateService() {
		c.Gateway.Runtime.StateProvider = profile.GatewayStateProvider
		c.Worker.StateProvider = profile.WorkerStateProvider
		return c
	}
	if gatewayUsesRemoteState(c.Gateway.Runtime) {
		if c.Gateway.Runtime.StateProvider == harnessruntime.RuntimeStateProviderModeSharedSQLite {
			c.Gateway.Runtime.StateProvider = profile.GatewayStateProvider
		}
		return c
	}
	if c.Gateway.Runtime.StateProvider == harnessruntime.RuntimeStateProviderModeSharedSQLite {
		return c.alignSharedStateStores()
	}
	return c
}
