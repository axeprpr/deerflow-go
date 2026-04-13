package stackcmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/internal/sandboxcmd"
	"github.com/axeprpr/deerflow-go/internal/statecmd"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type Config struct {
	Preset  StackPreset
	Gateway langgraphcmd.Config
	Worker  runtimecmd.NodeConfig
	State   statecmd.Config
	Sandbox sandboxcmd.Config
}

type StackPreset string

const (
	StackPresetAuto         StackPreset = "auto"
	StackPresetSharedSQLite StackPreset = "shared-sqlite"
	StackPresetSharedRemote StackPreset = "shared-remote"
)

func DefaultConfig() Config {
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

	return Config{
		Preset:  StackPresetSharedSQLite,
		Gateway: gateway,
		Worker:  worker,
		State:   state,
		Sandbox: sb,
	}
}

func (c Config) Validate() error {
	cfg := c.withDefaults()
	if cfg.Worker.TransportBackend == harnessruntime.WorkerTransportBackendRemote {
		return fmt.Errorf("invalid worker config: split stack worker transport cannot be remote")
	}
	if err := cfg.Worker.ValidateForRuntimeNode(); err != nil {
		return fmt.Errorf("invalid worker config: %w", err)
	}
	if err := cfg.Gateway.Validate(); err != nil {
		return fmt.Errorf("invalid gateway config: %w", err)
	}
	if cfg.usesDedicatedStateService() {
		if err := cfg.State.Validate(); err != nil {
			return fmt.Errorf("invalid state config: %w", err)
		}
		if err := cfg.Sandbox.Validate(); err != nil {
			return fmt.Errorf("invalid sandbox config: %w", err)
		}
	}
	return nil
}

func (c Config) withDefaults() Config {
	cfg := c
	cfg = cfg.applyPresetDefaults()
	if strings.TrimSpace(cfg.Worker.Provider) == "" {
		cfg.Worker.Provider = cfg.Gateway.Provider
	}
	if cfg.Worker.MaxTurns <= 0 {
		cfg.Worker.MaxTurns = cfg.Gateway.Runtime.MaxTurns
	}
	if strings.TrimSpace(cfg.Worker.DataRoot) == "" {
		cfg.Worker.DataRoot = cfg.Gateway.Runtime.DataRoot
	}
	if strings.TrimSpace(cfg.State.Runtime.DataRoot) == "" {
		cfg.State.Runtime.DataRoot = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.DataRoot), strings.TrimSpace(cfg.Worker.DataRoot))
	}
	if strings.TrimSpace(cfg.Sandbox.Runtime.DataRoot) == "" {
		cfg.Sandbox.Runtime.DataRoot = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.DataRoot), strings.TrimSpace(cfg.Worker.DataRoot))
	}
	if strings.TrimSpace(cfg.Worker.MemoryStoreURL) == "" {
		cfg.Worker.MemoryStoreURL = cfg.Gateway.Runtime.MemoryStoreURL
	}
	if cfg.Worker.StateProvider == "" {
		cfg.Worker.StateProvider = cfg.Gateway.Runtime.StateProvider
	}
	if strings.TrimSpace(cfg.Worker.StateRoot) == "" {
		cfg.Worker.StateRoot = cfg.Gateway.Runtime.StateRoot
	}
	if strings.TrimSpace(cfg.Worker.StateStoreURL) == "" && !gatewayUsesRemoteState(cfg.Gateway.Runtime) {
		cfg.Worker.StateStoreURL = cfg.Gateway.Runtime.StateStoreURL
	}
	if strings.TrimSpace(cfg.Worker.SnapshotStoreURL) == "" && !gatewayUsesRemoteState(cfg.Gateway.Runtime) {
		cfg.Worker.SnapshotStoreURL = cfg.Gateway.Runtime.SnapshotStoreURL
	}
	if strings.TrimSpace(cfg.Worker.EventStoreURL) == "" && !gatewayUsesRemoteState(cfg.Gateway.Runtime) {
		cfg.Worker.EventStoreURL = cfg.Gateway.Runtime.EventStoreURL
	}
	if strings.TrimSpace(cfg.Worker.ThreadStoreURL) == "" && !gatewayUsesRemoteState(cfg.Gateway.Runtime) {
		cfg.Worker.ThreadStoreURL = cfg.Gateway.Runtime.ThreadStoreURL
	}
	cfg.Worker.TransportBackend = firstNonEmptyTransport(cfg.Worker.TransportBackend, harnessruntime.WorkerTransportBackendQueue)
	if !gatewayUsesRemoteState(cfg.Gateway.Runtime) {
		cfg.Worker.StateBackend = firstNonEmptyState(cfg.Worker.StateBackend, cfg.Gateway.Runtime.StateBackend)
		cfg.Worker.SnapshotBackend = firstNonEmptyState(cfg.Worker.SnapshotBackend, cfg.Gateway.Runtime.SnapshotBackend)
		cfg.Worker.EventBackend = firstNonEmptyState(cfg.Worker.EventBackend, cfg.Gateway.Runtime.EventBackend)
		cfg.Worker.ThreadBackend = firstNonEmptyState(cfg.Worker.ThreadBackend, cfg.Gateway.Runtime.ThreadBackend)
	}

	cfg.Worker.Role = harnessruntime.RuntimeNodeRoleWorker
	cfg.Gateway.Runtime.Role = harnessruntime.RuntimeNodeRoleGateway
	cfg.Gateway.Runtime.Addr = cfg.Worker.Addr
	cfg.Gateway.Runtime.Provider = cfg.Worker.Provider
	cfg.Gateway.Runtime.MaxTurns = cfg.Worker.MaxTurns
	prevGatewayEndpoint := cfg.Gateway.Runtime.Endpoint
	prevGatewayStateStore := strings.TrimSpace(cfg.Gateway.Runtime.StateStoreURL)
	cfg.Gateway.Runtime.DataRoot = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.DataRoot), strings.TrimSpace(cfg.Worker.DataRoot))
	cfg.Gateway.Runtime.MemoryStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.MemoryStoreURL), strings.TrimSpace(cfg.Worker.MemoryStoreURL))
	if cfg.Gateway.Runtime.StateProvider == "" {
		cfg.Gateway.Runtime.StateProvider = cfg.Worker.StateProvider
	}
	if cfg.usesDedicatedStateService() {
		stateURL := stateServiceEndpoint(cfg.State.Runtime.Addr)
		cfg.State = cfg.State.WithDefaults()
		cfg.Sandbox = cfg.Sandbox.WithDefaults()
		cfg.Gateway.Runtime.StateProvider = harnessruntime.RuntimeStateProviderModeAuto
		cfg.Gateway.Runtime.StateBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Gateway.Runtime.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Gateway.Runtime.EventBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Gateway.Runtime.ThreadBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Gateway.Runtime.StateStoreURL = stateURL
		cfg.Gateway.Runtime.SnapshotStoreURL = ""
		cfg.Gateway.Runtime.EventStoreURL = ""
		cfg.Gateway.Runtime.ThreadStoreURL = ""
		cfg.Gateway.Runtime.StateRoot = ""

		cfg.Worker.StateProvider = harnessruntime.RuntimeStateProviderModeAuto
		cfg.Worker.StateBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Worker.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Worker.EventBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Worker.ThreadBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Worker.StateStoreURL = stateURL
		cfg.Worker.SnapshotStoreURL = ""
		cfg.Worker.EventStoreURL = ""
		cfg.Worker.ThreadStoreURL = ""
		cfg.Worker.StateRoot = ""
		cfg.Worker.SandboxBackend = harnessruntime.SandboxBackendRemote
		cfg.Worker.SandboxEndpoint = sandboxServiceEndpoint(cfg.Sandbox.Runtime.Addr)
	} else if gatewayUsesRemoteState(cfg.Gateway.Runtime) {
		cfg.Gateway.Runtime.StateBackend = harnessruntime.RuntimeStateStoreBackendRemote
		cfg.Gateway.Runtime.SnapshotBackend = firstNonEmptyState(cfg.Gateway.Runtime.SnapshotBackend, harnessruntime.RuntimeStateStoreBackendRemote)
		cfg.Gateway.Runtime.EventBackend = firstNonEmptyState(cfg.Gateway.Runtime.EventBackend, harnessruntime.RuntimeStateStoreBackendRemote)
		cfg.Gateway.Runtime.ThreadBackend = firstNonEmptyState(cfg.Gateway.Runtime.ThreadBackend, harnessruntime.RuntimeStateStoreBackendRemote)
		if prevGatewayStateStore == "" || prevGatewayStateStore == stateStoreEndpointFromWorkerDispatch(prevGatewayEndpoint) {
			cfg.Gateway.Runtime.StateStoreURL = workerStateEndpoint(cfg.Worker.Addr)
		} else {
			cfg.Gateway.Runtime.StateStoreURL = prevGatewayStateStore
		}
	} else {
		cfg.Gateway.Runtime.StateRoot = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.StateRoot), strings.TrimSpace(cfg.Worker.StateRoot))
		cfg.Gateway.Runtime.StateStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.StateStoreURL), strings.TrimSpace(cfg.Worker.StateStoreURL))
		cfg.Gateway.Runtime.SnapshotStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.SnapshotStoreURL), strings.TrimSpace(cfg.Worker.SnapshotStoreURL))
		cfg.Gateway.Runtime.EventStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.EventStoreURL), strings.TrimSpace(cfg.Worker.EventStoreURL))
		cfg.Gateway.Runtime.ThreadStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.ThreadStoreURL), strings.TrimSpace(cfg.Worker.ThreadStoreURL))
		cfg.Gateway.Runtime.StateBackend = firstNonEmptyState(cfg.Gateway.Runtime.StateBackend, cfg.Worker.StateBackend)
		cfg.Gateway.Runtime.SnapshotBackend = firstNonEmptyState(cfg.Gateway.Runtime.SnapshotBackend, cfg.Worker.SnapshotBackend)
		cfg.Gateway.Runtime.EventBackend = firstNonEmptyState(cfg.Gateway.Runtime.EventBackend, cfg.Worker.EventBackend)
		cfg.Gateway.Runtime.ThreadBackend = firstNonEmptyState(cfg.Gateway.Runtime.ThreadBackend, cfg.Worker.ThreadBackend)
	}
	cfg.Gateway.Runtime.Endpoint = workerDispatchEndpoint(cfg.Worker.Addr)

	if cfg.usesDedicatedStateService() {
		cfg.Gateway.Runtime.StateProvider = harnessruntime.RuntimeStateProviderModeAuto
		cfg.Worker.StateProvider = harnessruntime.RuntimeStateProviderModeAuto
	} else if gatewayUsesRemoteState(cfg.Gateway.Runtime) {
		if cfg.Gateway.Runtime.StateProvider == harnessruntime.RuntimeStateProviderModeSharedSQLite {
			cfg.Gateway.Runtime.StateProvider = harnessruntime.RuntimeStateProviderModeAuto
		}
	} else if cfg.Gateway.Runtime.StateProvider == harnessruntime.RuntimeStateProviderModeSharedSQLite {
		cfg = cfg.alignSharedStateStores()
	}
	return cfg
}

func (c Config) applyPresetDefaults() Config {
	switch c.effectivePreset() {
	case StackPresetSharedRemote:
		c.Gateway.Runtime.Preset = runtimecmd.RuntimeNodePresetSharedRemote
		c.Worker.Preset = runtimecmd.RuntimeNodePresetSharedSQLite
		c.State.Runtime.Preset = runtimecmd.RuntimeNodePresetSharedSQLite
		c.Sandbox.Runtime.Preset = runtimecmd.RuntimeNodePresetFastLocal
	case StackPresetSharedSQLite:
		c.Gateway.Runtime.Preset = runtimecmd.RuntimeNodePresetSharedSQLite
		c.Worker.Preset = runtimecmd.RuntimeNodePresetSharedSQLite
	default:
		if c.Gateway.Runtime.Preset == "" {
			c.Gateway.Runtime.Preset = runtimecmd.RuntimeNodePresetSharedSQLite
		}
		if c.Worker.Preset == "" {
			c.Worker.Preset = runtimecmd.RuntimeNodePresetSharedSQLite
		}
	}
	c.Gateway.Runtime = runtimecmd.ApplyNodePresetDefaults(c.Gateway.Runtime)
	c.Worker = runtimecmd.ApplyNodePresetDefaults(c.Worker)
	c.State.Runtime = runtimecmd.ApplyNodePresetDefaults(c.State.Runtime)
	c.Sandbox.Runtime = runtimecmd.ApplyNodePresetDefaults(c.Sandbox.Runtime)
	return c
}

func (c Config) effectivePreset() StackPreset {
	switch c.Preset {
	case StackPresetSharedSQLite, StackPresetSharedRemote:
		return c.Preset
	default:
		return StackPresetAuto
	}
}

func (c Config) alignSharedStateStores() Config {
	stateStore := firstNonEmpty(strings.TrimSpace(c.Gateway.Runtime.StateStoreURL), strings.TrimSpace(c.Worker.StateStoreURL))
	if stateStore == "" {
		return c
	}
	c.Gateway.Runtime.StateStoreURL = stateStore
	c.Worker.StateStoreURL = stateStore
	c.Gateway.Runtime.SnapshotStoreURL = stateStore
	c.Gateway.Runtime.EventStoreURL = stateStore
	c.Gateway.Runtime.ThreadStoreURL = stateStore
	c.Worker.SnapshotStoreURL = stateStore
	c.Worker.EventStoreURL = stateStore
	c.Worker.ThreadStoreURL = stateStore
	return c
}

func (c Config) StartupLines(build langgraphcmd.BuildInfo, yolo bool, logLevel string) []string {
	cfg := c.withDefaults()
	lines := cfg.Gateway.StartupLines(build, yolo, logLevel)
	lines = append(lines,
		fmt.Sprintf("Starting split runtime stack preset=%s...", cfg.effectivePreset()),
		fmt.Sprintf("  Worker: role=%s addr=%s transport=%s sandbox=%s state_provider=%s", cfg.Worker.Role, cfg.Worker.Addr, cfg.Worker.TransportBackend, cfg.Worker.SandboxBackend, cfg.Worker.StateProvider),
		fmt.Sprintf("  Shared state: root=%s store=%s snapshot=%s event=%s thread=%s", firstNonEmpty(cfg.Worker.StateRoot, "(memory)"), firstNonEmpty(cfg.Worker.StateStoreURL, "(derived)"), firstNonEmpty(cfg.Worker.SnapshotStoreURL, "(derived)"), firstNonEmpty(cfg.Worker.EventStoreURL, "(derived)"), firstNonEmpty(cfg.Worker.ThreadStoreURL, "(derived)")),
	)
	if cfg.usesDedicatedStateService() {
		lines = append(lines, fmt.Sprintf("  State service: addr=%s provider=%s store=%s", cfg.State.Runtime.Addr, cfg.State.Runtime.StateProvider, firstNonEmpty(cfg.State.Runtime.StateStoreURL, "(derived)")))
		lines = append(lines, fmt.Sprintf("  Sandbox service: addr=%s backend=%s", cfg.Sandbox.Runtime.Addr, cfg.Sandbox.Runtime.SandboxBackend))
	}
	return lines
}

func (c Config) ReadyLines() []string {
	cfg := c.withDefaults()
	lines := cfg.Gateway.ReadyLines()
	lines = append(lines, c.LaunchSpec().ReadyLines()...)
	return lines
}

func workerDispatchEndpoint(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return strings.TrimRight(addr, "/") + harnessruntime.DefaultRemoteWorkerDispatchPath
}

func workerStateEndpoint(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return strings.TrimRight(addr, "/") + harnessruntime.DefaultRemoteStateBasePath
}

func stateStoreEndpointFromWorkerDispatch(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if strings.HasSuffix(endpoint, harnessruntime.DefaultRemoteWorkerDispatchPath) {
		return strings.TrimSuffix(endpoint, harnessruntime.DefaultRemoteWorkerDispatchPath) + harnessruntime.DefaultRemoteStateBasePath
	}
	return strings.TrimRight(endpoint, "/") + harnessruntime.DefaultRemoteStateBasePath
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonEmptyState(values ...harnessruntime.RuntimeStateStoreBackend) harnessruntime.RuntimeStateStoreBackend {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyTransport(values ...harnessruntime.WorkerTransportBackend) harnessruntime.WorkerTransportBackend {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func gatewayUsesRemoteState(cfg runtimecmd.NodeConfig) bool {
	if cfg.StateBackend == harnessruntime.RuntimeStateStoreBackendRemote ||
		cfg.SnapshotBackend == harnessruntime.RuntimeStateStoreBackendRemote ||
		cfg.EventBackend == harnessruntime.RuntimeStateStoreBackendRemote ||
		cfg.ThreadBackend == harnessruntime.RuntimeStateStoreBackendRemote {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(cfg.StateStoreURL), "http://") || strings.HasPrefix(strings.TrimSpace(cfg.StateStoreURL), "https://") {
		return true
	}
	return false
}

func (c Config) usesDedicatedStateService() bool {
	return c.effectivePreset() == StackPresetSharedRemote
}

func stateRootFromDataRoot(dataRoot string) string {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" {
		return ""
	}
	return filepath.Join(dataRoot, "runtime-state")
}

func stateServiceEndpoint(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return strings.TrimRight(addr, "/") + harnessruntime.DefaultRemoteStateBasePath
}

func sandboxServiceEndpoint(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	return strings.TrimRight(addr, "/")
}
