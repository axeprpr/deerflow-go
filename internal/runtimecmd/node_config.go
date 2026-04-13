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
	Role     harnessruntime.RuntimeNodeRole
	Addr     string
	Name     string
	Root     string
	Endpoint string
	MaxTurns int
}

type NodeConfig struct {
	Role     harnessruntime.RuntimeNodeRole
	Addr     string
	Name     string
	Root     string
	DataRoot string
	Provider string
	Endpoint string
	MaxTurns int
}

func DefaultNodeConfig(defaults NodeDefaults) NodeConfig {
	role := NormalizeRole(os.Getenv("RUNTIME_NODE_ROLE"), defaults.Role)
	return NodeConfig{
		Role:     role,
		Addr:     NormalizeAddr(firstNonEmpty(os.Getenv("RUNTIME_NODE_ADDR"), defaults.Addr), defaults.Addr),
		Name:     firstNonEmpty(os.Getenv("RUNTIME_NODE_NAME"), defaults.Name),
		Root:     firstNonEmpty(os.Getenv("RUNTIME_NODE_ROOT"), defaults.Root),
		DataRoot: firstNonEmpty(os.Getenv("DEERFLOW_DATA_ROOT"), tools.DataRootFromEnv()),
		Provider: firstNonEmpty(os.Getenv("DEFAULT_LLM_PROVIDER"), "siliconflow"),
		Endpoint: strings.TrimSpace(firstNonEmpty(os.Getenv("RUNTIME_NODE_ENDPOINT"), defaults.Endpoint)),
		MaxTurns: intFromEnv("RUNTIME_NODE_MAX_TURNS", defaults.MaxTurns),
	}
}

func DefaultLangGraphNodeConfig() NodeConfig {
	return DefaultNodeConfig(NodeDefaults{
		Role:     harnessruntime.RuntimeNodeRoleAllInOne,
		Addr:     ":8081",
		Name:     "langgraph",
		Root:     filepath.Join(os.TempDir(), "deerflow-langgraph-sandbox"),
		Endpoint: "",
		MaxTurns: 100,
	})
}

func DefaultRuntimeWorkerNodeConfig() NodeConfig {
	return DefaultNodeConfig(NodeDefaults{
		Role:     harnessruntime.RuntimeNodeRoleWorker,
		Addr:     ":8081",
		Name:     "runtime-node",
		Root:     filepath.Join(os.TempDir(), "deerflow-runtime-node"),
		Endpoint: "",
		MaxTurns: 100,
	})
}

func (c NodeConfig) RuntimeNodeConfig() harnessruntime.RuntimeNodeConfig {
	name := strings.TrimSpace(c.Name)
	root := strings.TrimSpace(c.Root)
	switch c.Role {
	case harnessruntime.RuntimeNodeRoleGateway:
		config := harnessruntime.DefaultGatewayRuntimeNodeConfig(name, root, strings.TrimSpace(c.Endpoint))
		config.RemoteWorker.Addr = NormalizeAddr(c.Addr, config.RemoteWorker.Addr)
		return config
	case harnessruntime.RuntimeNodeRoleWorker:
		config := harnessruntime.DefaultWorkerRuntimeNodeConfig(name, root)
		config.RemoteWorker.Addr = NormalizeAddr(c.Addr, config.RemoteWorker.Addr)
		return config
	default:
		config := harnessruntime.DefaultRuntimeNodeConfig(name, root)
		config.RemoteWorker.Addr = NormalizeAddr(c.Addr, config.RemoteWorker.Addr)
		return config
	}
}

func (c NodeConfig) ValidateForLangGraph() error {
	switch c.Role {
	case harnessruntime.RuntimeNodeRoleAllInOne:
		return nil
	case harnessruntime.RuntimeNodeRoleGateway:
		if strings.TrimSpace(c.Endpoint) == "" {
			return fmt.Errorf("runtime gateway role requires a remote worker endpoint")
		}
		return nil
	default:
		return fmt.Errorf("langgraph command only supports runtime roles %q and %q", harnessruntime.RuntimeNodeRoleAllInOne, harnessruntime.RuntimeNodeRoleGateway)
	}
}

func (c NodeConfig) ValidateForRuntimeNode() error {
	switch c.Role {
	case harnessruntime.RuntimeNodeRoleAllInOne, harnessruntime.RuntimeNodeRoleWorker:
		return nil
	case harnessruntime.RuntimeNodeRoleGateway:
		return fmt.Errorf("gateway role does not start a runtime worker server; use cmd/langgraph for API serving")
	default:
		return fmt.Errorf("unsupported runtime node role %q", c.Role)
	}
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
