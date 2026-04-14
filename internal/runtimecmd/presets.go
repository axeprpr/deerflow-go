package runtimecmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func DefaultNodeConfigForRole(role harnessruntime.RuntimeNodeRole) NodeConfig {
	defaults := NodeDefaults{
		Preset:           RuntimeNodePresetAuto,
		Role:             role,
		Addr:             ":8081",
		MaxTurns:         100,
		TransportBackend: harnessruntime.WorkerTransportBackendQueue,
		SandboxBackend:   harnessruntime.SandboxBackendLocalLinux,
		StateBackend:     harnessruntime.RuntimeStateStoreBackendInMemory,
	}
	switch role {
	case harnessruntime.RuntimeNodeRoleGateway:
		defaults.Name = "langgraph-gateway"
		defaults.Root = filepath.Join(os.TempDir(), "deerflow-langgraph-sandbox")
	case harnessruntime.RuntimeNodeRoleWorker:
		defaults.Name = "runtime-node"
		defaults.Root = filepath.Join(os.TempDir(), "deerflow-runtime-node")
	default:
		defaults.Name = "langgraph"
		defaults.Root = filepath.Join(os.TempDir(), "deerflow-langgraph-sandbox")
	}
	return DefaultNodeConfig(defaults)
}

func DefaultSplitGatewayNodeConfig(endpoint string) NodeConfig {
	config := DefaultNodeConfigForRole(harnessruntime.RuntimeNodeRoleGateway)
	config.Preset = RuntimeNodePresetSharedSQLite
	config.Endpoint = strings.TrimSpace(endpoint)
	return ApplyNodePresetDefaults(config)
}

func DefaultSplitWorkerNodeConfig() NodeConfig {
	config := DefaultNodeConfigForRole(harnessruntime.RuntimeNodeRoleWorker)
	config.Preset = RuntimeNodePresetSharedSQLite
	return ApplyNodePresetDefaults(config)
}

func ApplyNodePresetDefaults(config NodeConfig) NodeConfig {
	config = applyNodePreset(config)
	return config.deriveStateBackendsFromStoreURLs()
}

func applyNodePreset(config NodeConfig) NodeConfig {
	if config.DataRoot == "" {
		config.DataRoot = tools.DataRootFromEnv()
	}
	if config.Preset == "" {
		config.Preset = RuntimeNodePresetAuto
	}
	return DefaultRuntimeProfile(config.effectivePreset(), config.Role).Apply(config)
}

func (c NodeConfig) applyFastLocalDefaults() NodeConfig {
	c.StateProvider = harnessruntime.RuntimeStateProviderModeIsolated
	if c.StateBackend == harnessruntime.RuntimeStateStoreBackendSQLite {
		c.StateBackend = harnessruntime.RuntimeStateStoreBackendInMemory
	}
	if c.SnapshotBackend == harnessruntime.RuntimeStateStoreBackendSQLite {
		c.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendInMemory
	}
	if c.EventBackend == harnessruntime.RuntimeStateStoreBackendSQLite {
		c.EventBackend = harnessruntime.RuntimeStateStoreBackendInMemory
	}
	if c.ThreadBackend == harnessruntime.RuntimeStateStoreBackendSQLite {
		c.ThreadBackend = harnessruntime.RuntimeStateStoreBackendInMemory
	}
	if c.StateBackend == harnessruntime.RuntimeStateStoreBackendInMemory {
		c.StateStoreURL = ""
	}
	if c.SnapshotBackend == harnessruntime.RuntimeStateStoreBackendInMemory {
		c.SnapshotStoreURL = ""
	}
	if c.EventBackend == harnessruntime.RuntimeStateStoreBackendInMemory {
		c.EventStoreURL = ""
	}
	if c.ThreadBackend == harnessruntime.RuntimeStateStoreBackendInMemory {
		c.ThreadStoreURL = ""
	}
	if c.StateRoot == "" && c.StateStoreURL == "" && c.SnapshotStoreURL == "" && c.EventStoreURL == "" && c.ThreadStoreURL == "" {
		c.StateRoot = ""
	}
	return c
}

func (c NodeConfig) applySharedRemoteDefaults() NodeConfig {
	switch c.Role {
	case harnessruntime.RuntimeNodeRoleGateway:
		c.StateProvider = harnessruntime.RuntimeStateProviderModeAuto
		c.StateBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.SnapshotBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.EventBackend = harnessruntime.RuntimeStateStoreBackendRemote
		c.ThreadBackend = harnessruntime.RuntimeStateStoreBackendRemote
		if !strings.HasPrefix(strings.TrimSpace(c.StateStoreURL), "http://") && !strings.HasPrefix(strings.TrimSpace(c.StateStoreURL), "https://") {
			c.StateStoreURL = stateStoreEndpointFromDispatchEndpoint(c.Endpoint)
		}
		c.SnapshotStoreURL = ""
		c.EventStoreURL = ""
		c.ThreadStoreURL = ""
		c.StateRoot = ""
	default:
		c = c.applySharedSQLiteDefaults()
	}
	return c
}
