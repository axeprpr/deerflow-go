package statecmd

import (
	"context"
	"fmt"

	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type Config struct {
	Runtime runtimecmd.NodeConfig
}

func DefaultConfig() Config {
	cfg := runtimecmd.DefaultNodeConfigForRole(harnessruntime.RuntimeNodeRoleWorker)
	cfg.Name = "runtime-state"
	cfg.Addr = ":8082"
	cfg.TransportBackend = harnessruntime.WorkerTransportBackendQueue
	cfg.Preset = runtimecmd.RuntimeNodePresetSharedSQLite
	cfg = runtimecmd.ApplyNodePresetDefaults(cfg)
	return Config{Runtime: cfg}
}

func (c Config) Validate() error {
	return c.Runtime.ValidateForStateServer()
}

func (c Config) BuildLauncher() (*harnessruntime.RuntimeStateLauncher, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c.Runtime.BuildStateLauncher()
}

func (c Config) StartupLines() []string {
	cfg := c.WithDefaults()
	return []string{
		fmt.Sprintf("runtime state server starting preset=%s", cfg.Runtime.Preset),
		fmt.Sprintf("  addr=%s", cfg.Runtime.Addr),
		fmt.Sprintf("  state_provider=%s state=%s snapshot=%s event=%s thread=%s root=%s store=%s", firstNonEmpty(string(cfg.Runtime.StateProvider), "(auto)"), firstNonEmpty(string(cfg.Runtime.StateBackend), "(default)"), firstNonEmpty(string(cfg.Runtime.SnapshotBackend), "(default)"), firstNonEmpty(string(cfg.Runtime.EventBackend), "(default)"), firstNonEmpty(string(cfg.Runtime.ThreadBackend), "(default)"), firstNonEmpty(cfg.Runtime.StateRoot, "(memory)"), firstNonEmpty(cfg.Runtime.StateStoreURL, "(derived)")),
		fmt.Sprintf("  snapshot_store=%s event_store=%s thread_store=%s", firstNonEmpty(cfg.Runtime.SnapshotStoreURL, "(derived)"), firstNonEmpty(cfg.Runtime.EventStoreURL, "(derived)"), firstNonEmpty(cfg.Runtime.ThreadStoreURL, "(derived)")),
	}
}

func (c Config) ReadyLines() []string {
	cfg := c.WithDefaults()
	return []string{fmt.Sprintf("runtime state ready addr=%s", cfg.Runtime.Addr)}
}

func (c Config) ReadyProbe() func(ctx context.Context) error {
	cfg := c.WithDefaults()
	return cfg.Runtime.StateReadyProbe()
}

func (c Config) WithDefaults() Config {
	cfg := c
	cfg.Runtime = runtimecmd.ApplyNodePresetDefaults(cfg.Runtime)
	if cfg.Runtime.Name == "" {
		cfg.Runtime.Name = "runtime-state"
	}
	if cfg.Runtime.Addr == "" {
		cfg.Runtime.Addr = ":8082"
	}
	return cfg
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
