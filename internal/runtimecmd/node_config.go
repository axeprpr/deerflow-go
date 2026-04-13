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
	StateRoot        string
	StateBackend     harnessruntime.RuntimeStateStoreBackend
	SnapshotBackend  harnessruntime.RuntimeStateStoreBackend
	EventBackend     harnessruntime.RuntimeStateStoreBackend
	ThreadBackend    harnessruntime.RuntimeStateStoreBackend
}

func DefaultNodeConfig(defaults NodeDefaults) NodeConfig {
	role := NormalizeRole(os.Getenv("RUNTIME_NODE_ROLE"), defaults.Role)
	return NodeConfig{
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
		StateRoot:        strings.TrimSpace(os.Getenv("RUNTIME_NODE_STATE_ROOT")),
		StateBackend:     NormalizeStateBackend(firstNonEmpty(os.Getenv("RUNTIME_NODE_STATE_BACKEND"), string(defaults.StateBackend)), defaults.StateBackend),
		SnapshotBackend:  NormalizeStateBackend(os.Getenv("RUNTIME_NODE_SNAPSHOT_BACKEND"), ""),
		EventBackend:     NormalizeStateBackend(os.Getenv("RUNTIME_NODE_EVENT_BACKEND"), ""),
		ThreadBackend:    NormalizeStateBackend(os.Getenv("RUNTIME_NODE_THREAD_BACKEND"), ""),
	}
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
	if c.StateBackend != "" {
		config.State.Backend = NormalizeStateBackend(string(c.StateBackend), config.State.Backend)
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
	if strings.TrimSpace(c.StateRoot) != "" {
		config.State.Root = strings.TrimSpace(c.StateRoot)
	} else if usesFileStateBackend(config.State) {
		config.State.Root = filepath.Join(root, "state")
	}
	return config
}

func (c NodeConfig) ValidateForLangGraph() error {
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

func usesFileStateBackend(config harnessruntime.RuntimeStateStoreConfig) bool {
	backends := []harnessruntime.RuntimeStateStoreBackend{
		config.Backend,
		config.SnapshotBackend,
		config.EventBackend,
		config.ThreadBackend,
	}
	for _, backend := range backends {
		if backend == harnessruntime.RuntimeStateStoreBackendFile {
			return true
		}
	}
	return false
}
