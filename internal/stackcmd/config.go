package stackcmd

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
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
	gateway, worker, state, sb := defaultSplitComponents()

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
	prevGatewayEndpoint := cfg.Gateway.Runtime.Endpoint
	prevGatewayStateStore := strings.TrimSpace(cfg.Gateway.Runtime.StateStoreURL)
	cfg = cfg.applySplitIdentityDefaults()
	cfg = cfg.applySplitStoreDefaults()
	cfg = cfg.applySplitTopologyDefaults()
	cfg = cfg.applyDedicatedRemoteServices(prevGatewayEndpoint, prevGatewayStateStore)
	cfg = cfg.applyStateProviderDefaults()
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
	spec := cfg.DeploymentSpec()
	lines := cfg.Gateway.StartupLines(build, yolo, logLevel)
	lines = append(lines,
		fmt.Sprintf("Starting split runtime stack preset=%s...", cfg.effectivePreset()),
		fmt.Sprintf("  Worker: role=%s addr=%s transport=%s sandbox=%s state_provider=%s", cfg.Worker.Role, cfg.Worker.Addr, cfg.Worker.TransportBackend, cfg.Worker.SandboxBackend, cfg.Worker.StateProvider),
		fmt.Sprintf("  Shared state: root=%s store=%s snapshot=%s event=%s thread=%s", firstNonEmpty(cfg.Worker.StateRoot, "(memory)"), firstNonEmpty(cfg.Worker.StateStoreURL, "(derived)"), firstNonEmpty(cfg.Worker.SnapshotStoreURL, "(derived)"), firstNonEmpty(cfg.Worker.EventStoreURL, "(derived)"), firstNonEmpty(cfg.Worker.ThreadStoreURL, "(derived)")),
	)
	if cfg.usesDedicatedStateService() {
		for _, component := range spec.Components {
			switch component.Kind {
			case ComponentState:
				lines = append(lines, fmt.Sprintf("  State service: addr=%s provider=%s store=%s", component.Addr, cfg.State.Runtime.StateProvider, firstNonEmpty(cfg.State.Runtime.StateStoreURL, "(derived)")))
			case ComponentSandbox:
				lines = append(lines, fmt.Sprintf("  Sandbox service: addr=%s backend=%s", component.Addr, cfg.Sandbox.Runtime.SandboxBackend))
			}
		}
	}
	return lines
}

func (c Config) ReadyLines() []string {
	cfg := c.withDefaults()
	lines := cfg.Gateway.ReadyLines()
	lines = append(lines, cfg.DeploymentSpec().ReadyLines()...)
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

const (
	defaultStateServiceAddr   = ":8082"
	defaultSandboxServiceAddr = ":8083"
)

func deriveDedicatedServiceAddr(workerAddr, currentAddr, defaultAddr string, portOffset int) string {
	currentAddr = strings.TrimSpace(currentAddr)
	if currentAddr != "" && currentAddr != defaultAddr {
		return currentAddr
	}
	derived, ok := deriveAddrWithPortOffset(workerAddr, portOffset)
	if ok {
		return derived
	}
	if currentAddr != "" {
		return currentAddr
	}
	return defaultAddr
}

func deriveAddrWithPortOffset(addr string, portOffset int) (string, bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", false
	}
	host, port, err := splitHostPort(addr)
	if err != nil {
		return "", false
	}
	nextPort, err := strconv.Atoi(port)
	if err != nil {
		return "", false
	}
	nextPort += portOffset
	if nextPort <= 0 || nextPort > 65535 {
		return "", false
	}
	if host == "" {
		return ":" + strconv.Itoa(nextPort), true
	}
	return net.JoinHostPort(host, strconv.Itoa(nextPort)), true
}

func splitHostPort(addr string) (string, string, error) {
	if strings.HasPrefix(addr, ":") {
		return "", strings.TrimPrefix(addr, ":"), nil
	}
	if !strings.Contains(addr, ":") {
		return "", "", fmt.Errorf("missing port")
	}
	host, port, err := net.SplitHostPort(addr)
	if err == nil {
		return host, port, nil
	}
	lastColon := strings.LastIndex(addr, ":")
	if lastColon <= 0 || lastColon == len(addr)-1 {
		return "", "", err
	}
	return addr[:lastColon], addr[lastColon+1:], nil
}
