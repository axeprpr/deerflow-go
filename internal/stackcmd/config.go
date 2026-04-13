package stackcmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type Config struct {
	Gateway langgraphcmd.Config
	Worker  runtimecmd.NodeConfig
}

func DefaultConfig() Config {
	gateway := langgraphcmd.DefaultConfig()
	worker := runtimecmd.DefaultRuntimeWorkerNodeConfig()

	sharedDataRoot := firstNonEmpty(strings.TrimSpace(gateway.Runtime.DataRoot), strings.TrimSpace(worker.DataRoot))
	if sharedDataRoot != "" {
		gateway.Runtime.DataRoot = sharedDataRoot
		worker.DataRoot = sharedDataRoot
	}
	worker.Provider = gateway.Provider
	worker.MaxTurns = gateway.Runtime.MaxTurns
	worker.StateBackend = gateway.Runtime.StateBackend
	worker.SnapshotBackend = gateway.Runtime.SnapshotBackend
	worker.EventBackend = gateway.Runtime.EventBackend
	worker.ThreadBackend = gateway.Runtime.ThreadBackend
	worker.StateRoot = gateway.Runtime.StateRoot
	worker.SnapshotStoreURL = gateway.Runtime.SnapshotStoreURL
	worker.EventStoreURL = gateway.Runtime.EventStoreURL
	worker.ThreadStoreURL = gateway.Runtime.ThreadStoreURL
	worker.MemoryStoreURL = gateway.Runtime.MemoryStoreURL

	gateway.Runtime.Role = harnessruntime.RuntimeNodeRoleGateway
	gateway.Runtime.Addr = worker.Addr
	gateway.Runtime.Endpoint = workerDispatchEndpoint(worker.Addr)
	gateway.Runtime.Provider = worker.Provider

	return Config{
		Gateway: gateway,
		Worker:  worker,
	}
}

func (c Config) Validate() error {
	if err := c.Worker.ValidateForRuntimeNode(); err != nil {
		return fmt.Errorf("invalid worker config: %w", err)
	}
	if err := c.Gateway.Validate(); err != nil {
		return fmt.Errorf("invalid gateway config: %w", err)
	}
	return nil
}

func (c Config) withDefaults() Config {
	cfg := c
	if strings.TrimSpace(cfg.Worker.Provider) == "" {
		cfg.Worker.Provider = cfg.Gateway.Provider
	}
	if cfg.Worker.MaxTurns <= 0 {
		cfg.Worker.MaxTurns = cfg.Gateway.Runtime.MaxTurns
	}
	if strings.TrimSpace(cfg.Worker.DataRoot) == "" {
		cfg.Worker.DataRoot = cfg.Gateway.Runtime.DataRoot
	}
	if strings.TrimSpace(cfg.Worker.MemoryStoreURL) == "" {
		cfg.Worker.MemoryStoreURL = cfg.Gateway.Runtime.MemoryStoreURL
	}
	if strings.TrimSpace(cfg.Worker.StateRoot) == "" {
		cfg.Worker.StateRoot = cfg.Gateway.Runtime.StateRoot
	}
	if strings.TrimSpace(cfg.Worker.SnapshotStoreURL) == "" {
		cfg.Worker.SnapshotStoreURL = cfg.Gateway.Runtime.SnapshotStoreURL
	}
	if strings.TrimSpace(cfg.Worker.EventStoreURL) == "" {
		cfg.Worker.EventStoreURL = cfg.Gateway.Runtime.EventStoreURL
	}
	if strings.TrimSpace(cfg.Worker.ThreadStoreURL) == "" {
		cfg.Worker.ThreadStoreURL = cfg.Gateway.Runtime.ThreadStoreURL
	}

	cfg.Worker.Role = harnessruntime.RuntimeNodeRoleWorker
	cfg.Gateway.Runtime.Role = harnessruntime.RuntimeNodeRoleGateway
	cfg.Gateway.Runtime.Addr = cfg.Worker.Addr
	cfg.Gateway.Runtime.Provider = cfg.Worker.Provider
	cfg.Gateway.Runtime.MaxTurns = cfg.Worker.MaxTurns
	cfg.Gateway.Runtime.DataRoot = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.DataRoot), strings.TrimSpace(cfg.Worker.DataRoot))
	cfg.Gateway.Runtime.MemoryStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.MemoryStoreURL), strings.TrimSpace(cfg.Worker.MemoryStoreURL))
	cfg.Gateway.Runtime.StateRoot = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.StateRoot), strings.TrimSpace(cfg.Worker.StateRoot))
	cfg.Gateway.Runtime.SnapshotStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.SnapshotStoreURL), strings.TrimSpace(cfg.Worker.SnapshotStoreURL))
	cfg.Gateway.Runtime.EventStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.EventStoreURL), strings.TrimSpace(cfg.Worker.EventStoreURL))
	cfg.Gateway.Runtime.ThreadStoreURL = firstNonEmpty(strings.TrimSpace(cfg.Gateway.Runtime.ThreadStoreURL), strings.TrimSpace(cfg.Worker.ThreadStoreURL))
	cfg.Gateway.Runtime.StateBackend = firstNonEmptyState(cfg.Gateway.Runtime.StateBackend, cfg.Worker.StateBackend)
	cfg.Gateway.Runtime.SnapshotBackend = firstNonEmptyState(cfg.Gateway.Runtime.SnapshotBackend, cfg.Worker.SnapshotBackend)
	cfg.Gateway.Runtime.EventBackend = firstNonEmptyState(cfg.Gateway.Runtime.EventBackend, cfg.Worker.EventBackend)
	cfg.Gateway.Runtime.ThreadBackend = firstNonEmptyState(cfg.Gateway.Runtime.ThreadBackend, cfg.Worker.ThreadBackend)
	cfg.Gateway.Runtime.Endpoint = workerDispatchEndpoint(cfg.Worker.Addr)
	return cfg
}

func (c Config) StartupLines(build langgraphcmd.BuildInfo, yolo bool, logLevel string) []string {
	cfg := c.withDefaults()
	lines := cfg.Gateway.StartupLines(build, yolo, logLevel)
	lines = append(lines,
		"Starting split runtime stack...",
		fmt.Sprintf("  Worker: role=%s addr=%s transport=%s sandbox=%s", cfg.Worker.Role, cfg.Worker.Addr, cfg.Worker.TransportBackend, cfg.Worker.SandboxBackend),
		fmt.Sprintf("  Shared state: root=%s snapshot=%s event=%s thread=%s", firstNonEmpty(cfg.Worker.StateRoot, "(memory)"), firstNonEmpty(cfg.Worker.SnapshotStoreURL, "(derived)"), firstNonEmpty(cfg.Worker.EventStoreURL, "(derived)"), firstNonEmpty(cfg.Worker.ThreadStoreURL, "(derived)")),
	)
	return lines
}

func (c Config) ReadyLines() []string {
	cfg := c.withDefaults()
	lines := cfg.Gateway.ReadyLines()
	lines = append(lines, fmt.Sprintf("  Worker server: http://%s%s", cfg.Worker.Addr, harnessruntime.DefaultRemoteWorkerDispatchPath))
	lines = append(lines, fmt.Sprintf("  Worker sandbox: http://%s%s", cfg.Worker.Addr, harnessruntime.DefaultRemoteSandboxHealthPath))
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

func stateRootFromDataRoot(dataRoot string) string {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" {
		return ""
	}
	return filepath.Join(dataRoot, "runtime-state")
}
