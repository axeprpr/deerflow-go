package runtimecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type NodeDefaults struct {
	Role             harnessruntime.RuntimeNodeRole
	Addr             string
	Name             string
	Root             string
	Endpoint         string
	MaxTurns         int
	TransportBackend harnessruntime.WorkerTransportBackend
	SandboxBackend   harnessruntime.SandboxBackend
	StateBackend     harnessruntime.RuntimeStateStoreBackend
}

type NodeConfig struct {
	Role             harnessruntime.RuntimeNodeRole
	Addr             string
	Name             string
	Root             string
	DataRoot         string
	Provider         string
	Endpoint         string
	MaxTurns         int
	TransportBackend harnessruntime.WorkerTransportBackend
	SandboxBackend   harnessruntime.SandboxBackend
	SandboxEndpoint  string
	SandboxImage     string
	MemoryStoreURL   string
	StateRoot        string
	StateBackend     harnessruntime.RuntimeStateStoreBackend
	StateStoreURL    string
	SnapshotBackend  harnessruntime.RuntimeStateStoreBackend
	EventBackend     harnessruntime.RuntimeStateStoreBackend
	ThreadBackend    harnessruntime.RuntimeStateStoreBackend
	SnapshotStoreURL string
	EventStoreURL    string
	ThreadStoreURL   string
}

func DefaultNodeConfig(defaults NodeDefaults) NodeConfig {
	role := NormalizeRole(os.Getenv("RUNTIME_NODE_ROLE"), defaults.Role)
	config := NodeConfig{
		Role:             role,
		Addr:             NormalizeAddr(firstNonEmpty(os.Getenv("RUNTIME_NODE_ADDR"), defaults.Addr), defaults.Addr),
		Name:             firstNonEmpty(os.Getenv("RUNTIME_NODE_NAME"), defaults.Name),
		Root:             firstNonEmpty(os.Getenv("RUNTIME_NODE_ROOT"), defaults.Root),
		DataRoot:         firstNonEmpty(os.Getenv("DEERFLOW_DATA_ROOT"), tools.DataRootFromEnv()),
		Provider:         firstNonEmpty(os.Getenv("DEFAULT_LLM_PROVIDER"), "siliconflow"),
		Endpoint:         strings.TrimSpace(firstNonEmpty(os.Getenv("RUNTIME_NODE_ENDPOINT"), defaults.Endpoint)),
		MaxTurns:         intFromEnv("RUNTIME_NODE_MAX_TURNS", defaults.MaxTurns),
		TransportBackend: NormalizeTransportBackend(firstNonEmpty(os.Getenv("RUNTIME_NODE_TRANSPORT_BACKEND"), string(defaults.TransportBackend)), defaults.TransportBackend),
		SandboxBackend:   NormalizeSandboxBackend(firstNonEmpty(os.Getenv("RUNTIME_NODE_SANDBOX_BACKEND"), string(defaults.SandboxBackend)), defaults.SandboxBackend),
		SandboxEndpoint:  strings.TrimSpace(os.Getenv("RUNTIME_NODE_SANDBOX_ENDPOINT")),
		SandboxImage:     strings.TrimSpace(os.Getenv("RUNTIME_NODE_SANDBOX_IMAGE")),
		MemoryStoreURL:   strings.TrimSpace(os.Getenv("RUNTIME_NODE_MEMORY_STORE")),
		StateRoot:        strings.TrimSpace(os.Getenv("RUNTIME_NODE_STATE_ROOT")),
		StateBackend:     NormalizeStateBackend(firstNonEmpty(os.Getenv("RUNTIME_NODE_STATE_BACKEND"), string(defaults.StateBackend)), defaults.StateBackend),
		StateStoreURL:    strings.TrimSpace(os.Getenv("RUNTIME_NODE_STATE_STORE")),
		SnapshotBackend:  NormalizeStateBackend(os.Getenv("RUNTIME_NODE_SNAPSHOT_BACKEND"), ""),
		EventBackend:     NormalizeStateBackend(os.Getenv("RUNTIME_NODE_EVENT_BACKEND"), ""),
		ThreadBackend:    NormalizeStateBackend(os.Getenv("RUNTIME_NODE_THREAD_BACKEND"), ""),
		SnapshotStoreURL: strings.TrimSpace(os.Getenv("RUNTIME_NODE_SNAPSHOT_STORE")),
		EventStoreURL:    strings.TrimSpace(os.Getenv("RUNTIME_NODE_EVENT_STORE")),
		ThreadStoreURL:   strings.TrimSpace(os.Getenv("RUNTIME_NODE_THREAD_STORE")),
	}
	return config.withRoleDefaults()
}

func DefaultLangGraphNodeConfig() NodeConfig {
	return DefaultNodeConfig(NodeDefaults{
		Role:             harnessruntime.RuntimeNodeRoleAllInOne,
		Addr:             ":8081",
		Name:             "langgraph",
		Root:             filepath.Join(os.TempDir(), "deerflow-langgraph-sandbox"),
		Endpoint:         "",
		MaxTurns:         100,
		TransportBackend: harnessruntime.WorkerTransportBackendQueue,
		SandboxBackend:   harnessruntime.SandboxBackendLocalLinux,
		StateBackend:     harnessruntime.RuntimeStateStoreBackendInMemory,
	})
}

func DefaultRuntimeWorkerNodeConfig() NodeConfig {
	return DefaultNodeConfig(NodeDefaults{
		Role:             harnessruntime.RuntimeNodeRoleWorker,
		Addr:             ":8081",
		Name:             "runtime-node",
		Root:             filepath.Join(os.TempDir(), "deerflow-runtime-node"),
		Endpoint:         "",
		MaxTurns:         100,
		TransportBackend: harnessruntime.WorkerTransportBackendQueue,
		SandboxBackend:   harnessruntime.SandboxBackendLocalLinux,
		StateBackend:     harnessruntime.RuntimeStateStoreBackendInMemory,
	})
}

func (c NodeConfig) RuntimeNodeConfig() harnessruntime.RuntimeNodeConfig {
	name := strings.TrimSpace(c.Name)
	root := strings.TrimSpace(c.Root)
	var config harnessruntime.RuntimeNodeConfig
	switch c.Role {
	case harnessruntime.RuntimeNodeRoleGateway:
		config = harnessruntime.DefaultGatewayRuntimeNodeConfig(name, root, strings.TrimSpace(c.Endpoint))
	case harnessruntime.RuntimeNodeRoleWorker:
		config = harnessruntime.DefaultWorkerRuntimeNodeConfig(name, root)
	default:
		config = harnessruntime.DefaultRuntimeNodeConfig(name, root)
	}
	config.RemoteWorker.Addr = NormalizeAddr(c.Addr, config.RemoteWorker.Addr)
	if c.TransportBackend != "" {
		config.Transport.Backend = NormalizeTransportBackend(string(c.TransportBackend), config.Transport.Backend)
	}
	if strings.TrimSpace(c.Endpoint) != "" {
		config.Transport.Endpoint = strings.TrimSpace(c.Endpoint)
	}
	if c.SandboxBackend != "" {
		config.Sandbox.Backend = NormalizeSandboxBackend(string(c.SandboxBackend), config.Sandbox.Backend)
	}
	if strings.TrimSpace(c.SandboxEndpoint) != "" {
		config.Sandbox.Endpoint = strings.TrimSpace(c.SandboxEndpoint)
	}
	if strings.TrimSpace(c.SandboxImage) != "" {
		config.Sandbox.Image = strings.TrimSpace(c.SandboxImage)
	}
	if strings.TrimSpace(c.MemoryStoreURL) != "" {
		config.Memory.StoreURL = strings.TrimSpace(c.MemoryStoreURL)
	}
	if c.StateBackend != "" {
		config.State.Backend = NormalizeStateBackend(string(c.StateBackend), config.State.Backend)
	}
	if strings.TrimSpace(c.StateStoreURL) != "" {
		config.State.URL = strings.TrimSpace(c.StateStoreURL)
	}
	if c.SnapshotBackend != "" {
		config.State.SnapshotBackend = NormalizeStateBackend(string(c.SnapshotBackend), config.State.SnapshotBackend)
	}
	if c.EventBackend != "" {
		config.State.EventBackend = NormalizeStateBackend(string(c.EventBackend), config.State.EventBackend)
	}
	if c.ThreadBackend != "" {
		config.State.ThreadBackend = NormalizeStateBackend(string(c.ThreadBackend), config.State.ThreadBackend)
	}
	if strings.TrimSpace(c.SnapshotStoreURL) != "" {
		config.State.SnapshotURL = strings.TrimSpace(c.SnapshotStoreURL)
	}
	if strings.TrimSpace(c.EventStoreURL) != "" {
		config.State.EventURL = strings.TrimSpace(c.EventStoreURL)
	}
	if strings.TrimSpace(c.ThreadStoreURL) != "" {
		config.State.ThreadURL = strings.TrimSpace(c.ThreadStoreURL)
	}
	if strings.TrimSpace(c.StateRoot) != "" {
		config.State.Root = strings.TrimSpace(c.StateRoot)
	} else if usesPersistentStateBackend(config.State) {
		config.State.Root = filepath.Join(root, "state")
	}
	return config
}

func (c NodeConfig) withRoleDefaults() NodeConfig {
	if c.DataRoot == "" {
		c.DataRoot = tools.DataRootFromEnv()
	}
	switch c.Role {
	case harnessruntime.RuntimeNodeRoleGateway, harnessruntime.RuntimeNodeRoleWorker:
		if c.StateBackend == "" || c.StateBackend == harnessruntime.RuntimeStateStoreBackendInMemory {
			c.StateBackend = harnessruntime.RuntimeStateStoreBackendSQLite
		}
		if strings.TrimSpace(c.StateRoot) == "" && strings.TrimSpace(c.DataRoot) != "" && usesPersistentStateBackend(harnessruntime.RuntimeStateStoreConfig{
			Backend:         c.StateBackend,
			SnapshotBackend: c.SnapshotBackend,
			EventBackend:    c.EventBackend,
			ThreadBackend:   c.ThreadBackend,
			URL:             c.StateStoreURL,
			SnapshotURL:     c.SnapshotStoreURL,
			EventURL:        c.EventStoreURL,
			ThreadURL:       c.ThreadStoreURL,
		}) {
			c.StateRoot = filepath.Join(strings.TrimSpace(c.DataRoot), "runtime-state")
		}
		if strings.TrimSpace(c.DataRoot) != "" {
			stateRoot := firstNonEmpty(strings.TrimSpace(c.StateRoot), filepath.Join(strings.TrimSpace(c.DataRoot), "runtime-state"))
			if strings.TrimSpace(c.StateStoreURL) == "" && c.StateBackend == harnessruntime.RuntimeStateStoreBackendSQLite {
				c.StateStoreURL = "sqlite://" + filepath.Join(stateRoot, "runtime.sqlite3")
			}
			if strings.TrimSpace(c.StateStoreURL) != "" && c.StateBackend == harnessruntime.RuntimeStateStoreBackendSQLite {
				if strings.TrimSpace(c.SnapshotStoreURL) == "" && effectiveStateBackend(c.SnapshotBackend, c.StateBackend) == harnessruntime.RuntimeStateStoreBackendSQLite {
					c.SnapshotStoreURL = c.StateStoreURL
				}
				if strings.TrimSpace(c.EventStoreURL) == "" && effectiveStateBackend(c.EventBackend, c.StateBackend) == harnessruntime.RuntimeStateStoreBackendSQLite {
					c.EventStoreURL = c.StateStoreURL
				}
				if strings.TrimSpace(c.ThreadStoreURL) == "" && effectiveStateBackend(c.ThreadBackend, c.StateBackend) == harnessruntime.RuntimeStateStoreBackendSQLite {
					c.ThreadStoreURL = c.StateStoreURL
				}
			}
			if strings.TrimSpace(c.SnapshotStoreURL) == "" && effectiveStateBackend(c.SnapshotBackend, c.StateBackend) == harnessruntime.RuntimeStateStoreBackendSQLite {
				c.SnapshotStoreURL = "sqlite://" + filepath.Join(stateRoot, "snapshots.sqlite3")
			}
			if strings.TrimSpace(c.EventStoreURL) == "" && effectiveStateBackend(c.EventBackend, c.StateBackend) == harnessruntime.RuntimeStateStoreBackendSQLite {
				c.EventStoreURL = "sqlite://" + filepath.Join(stateRoot, "events.sqlite3")
			}
			if strings.TrimSpace(c.ThreadStoreURL) == "" && effectiveStateBackend(c.ThreadBackend, c.StateBackend) == harnessruntime.RuntimeStateStoreBackendSQLite {
				c.ThreadStoreURL = "sqlite://" + filepath.Join(stateRoot, "threads.sqlite3")
			}
		}
		if strings.TrimSpace(c.MemoryStoreURL) == "" && strings.TrimSpace(c.DataRoot) != "" {
			c.MemoryStoreURL = "sqlite://" + filepath.Join(strings.TrimSpace(c.DataRoot), "memory.sqlite3")
		}
	}
	return c
}

func (c NodeConfig) ValidateForLangGraph() error {
	if err := c.validateStateConfig(); err != nil {
		return err
	}
	node := c.RuntimeNodeConfig()
	switch c.Role {
	case harnessruntime.RuntimeNodeRoleAllInOne:
	case harnessruntime.RuntimeNodeRoleGateway:
		if strings.TrimSpace(node.Transport.Endpoint) == "" {
			return fmt.Errorf("runtime gateway role requires a remote worker endpoint")
		}
	default:
		return fmt.Errorf("langgraph command only supports runtime roles %q and %q", harnessruntime.RuntimeNodeRoleAllInOne, harnessruntime.RuntimeNodeRoleGateway)
	}
	if node.Transport.Backend == harnessruntime.WorkerTransportBackendRemote && strings.TrimSpace(node.Transport.Endpoint) == "" {
		return fmt.Errorf("remote worker transport requires endpoint")
	}
	return node.Sandbox.Validate()
}

func (c NodeConfig) ValidateForRuntimeNode() error {
	switch c.Role {
	case harnessruntime.RuntimeNodeRoleAllInOne, harnessruntime.RuntimeNodeRoleWorker:
	case harnessruntime.RuntimeNodeRoleGateway:
		return fmt.Errorf("gateway role does not start a runtime worker server; use cmd/langgraph for API serving")
	default:
		return fmt.Errorf("unsupported runtime node role %q", c.Role)
	}
	if err := c.validateStateConfig(); err != nil {
		return err
	}
	node := c.RuntimeNodeConfig()
	if node.Transport.Backend == harnessruntime.WorkerTransportBackendRemote && strings.TrimSpace(node.Transport.Endpoint) == "" {
		return fmt.Errorf("remote worker transport requires endpoint")
	}
	return node.Sandbox.Validate()
}

func NormalizeRole(value string, fallback harnessruntime.RuntimeNodeRole) harnessruntime.RuntimeNodeRole {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(harnessruntime.RuntimeNodeRoleAllInOne):
		return harnessruntime.RuntimeNodeRoleAllInOne
	case string(harnessruntime.RuntimeNodeRoleGateway):
		return harnessruntime.RuntimeNodeRoleGateway
	case string(harnessruntime.RuntimeNodeRoleWorker):
		return harnessruntime.RuntimeNodeRoleWorker
	default:
		return fallback
	}
}

func NormalizeAddr(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		value = ":8081"
	}
	if strings.HasPrefix(value, ":") {
		return value
	}
	return ":" + value
}

func NormalizeTransportBackend(value string, fallback harnessruntime.WorkerTransportBackend) harnessruntime.WorkerTransportBackend {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(harnessruntime.WorkerTransportBackendDirect):
		return harnessruntime.WorkerTransportBackendDirect
	case string(harnessruntime.WorkerTransportBackendRemote):
		return harnessruntime.WorkerTransportBackendRemote
	case string(harnessruntime.WorkerTransportBackendQueue):
		return harnessruntime.WorkerTransportBackendQueue
	default:
		return fallback
	}
}

func NormalizeSandboxBackend(value string, fallback harnessruntime.SandboxBackend) harnessruntime.SandboxBackend {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(harnessruntime.SandboxBackendLocalLinux):
		return harnessruntime.SandboxBackendLocalLinux
	case string(harnessruntime.SandboxBackendContainer):
		return harnessruntime.SandboxBackendContainer
	case string(harnessruntime.SandboxBackendRemote):
		return harnessruntime.SandboxBackendRemote
	case string(harnessruntime.SandboxBackendWindowsRestricted):
		return harnessruntime.SandboxBackendWindowsRestricted
	default:
		return fallback
	}
}

func NormalizeStateBackend(value string, fallback harnessruntime.RuntimeStateStoreBackend) harnessruntime.RuntimeStateStoreBackend {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(harnessruntime.RuntimeStateStoreBackendSQLite):
		return harnessruntime.RuntimeStateStoreBackendSQLite
	case string(harnessruntime.RuntimeStateStoreBackendFile):
		return harnessruntime.RuntimeStateStoreBackendFile
	case string(harnessruntime.RuntimeStateStoreBackendInMemory):
		return harnessruntime.RuntimeStateStoreBackendInMemory
	default:
		return fallback
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func intFromEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return fallback
	}
	return value
}

func usesPersistentStateBackend(config harnessruntime.RuntimeStateStoreConfig) bool {
	backends := []harnessruntime.RuntimeStateStoreBackend{
		config.Backend,
		config.SnapshotBackend,
		config.EventBackend,
		config.ThreadBackend,
	}
	for _, backend := range backends {
		if backend == harnessruntime.RuntimeStateStoreBackendFile || backend == harnessruntime.RuntimeStateStoreBackendSQLite {
			return true
		}
	}
	return false
}

func effectiveStateBackend(override harnessruntime.RuntimeStateStoreBackend, fallback harnessruntime.RuntimeStateStoreBackend) harnessruntime.RuntimeStateStoreBackend {
	if override != "" {
		return override
	}
	return fallback
}

func (c NodeConfig) validateStateConfig() error {
	if strings.TrimSpace(c.StateStoreURL) == "" {
		return nil
	}
	if effectiveStateBackend(c.SnapshotBackend, c.StateBackend) != harnessruntime.RuntimeStateStoreBackendSQLite ||
		effectiveStateBackend(c.EventBackend, c.StateBackend) != harnessruntime.RuntimeStateStoreBackendSQLite ||
		effectiveStateBackend(c.ThreadBackend, c.StateBackend) != harnessruntime.RuntimeStateStoreBackendSQLite {
		return fmt.Errorf("state-store requires sqlite snapshot/event/thread backends")
	}
	stateStore := normalizeStoreLocation(c.StateStoreURL)
	for name, value := range map[string]string{
		"snapshot-store": c.SnapshotStoreURL,
		"event-store":    c.EventStoreURL,
		"thread-store":   c.ThreadStoreURL,
	} {
		if trimmed := strings.TrimSpace(value); trimmed != "" && normalizeStoreLocation(trimmed) != stateStore {
			return fmt.Errorf("%s must match state-store when using shared sqlite state", name)
		}
	}
	return nil
}

func normalizeStoreLocation(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "sqlite://")
	trimmed = strings.TrimPrefix(trimmed, "file://")
	return trimmed
}
