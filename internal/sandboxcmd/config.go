package sandboxcmd

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
	cfg.Name = "runtime-sandbox"
	cfg.Addr = ":8083"
	cfg.Preset = runtimecmd.RuntimeNodePresetFastLocal
	cfg = runtimecmd.ApplyNodePresetDefaults(cfg)
	return Config{Runtime: cfg}
}

func (c Config) Validate() error {
	return c.Runtime.ValidateForSandboxServer()
}

func (c Config) BuildLauncher() (*harnessruntime.RuntimeSandboxLauncher, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c.Runtime.BuildSandboxLauncher()
}

func (c Config) StartupLines() []string {
	cfg := c.WithDefaults()
	return []string{
		fmt.Sprintf("runtime sandbox server starting preset=%s", cfg.Runtime.Preset),
		fmt.Sprintf("  addr=%s", cfg.Runtime.Addr),
		fmt.Sprintf("  backend=%s root=%s endpoint=%s image=%s", cfg.Runtime.SandboxBackend, firstNonEmpty(cfg.Runtime.Root, "(auto)"), firstNonEmpty(cfg.Runtime.SandboxEndpoint, "(none)"), firstNonEmpty(cfg.Runtime.SandboxImage, "(none)")),
	}
}

func (c Config) ReadyLines() []string {
	cfg := c.WithDefaults()
	return []string{fmt.Sprintf("runtime sandbox ready addr=%s", cfg.Runtime.Addr)}
}

func (c Config) ReadyProbe() func(ctx context.Context) error {
	cfg := c.WithDefaults()
	return cfg.Runtime.SandboxReadyProbe()
}

func (c Config) WithDefaults() Config {
	cfg := c
	cfg.Runtime = runtimecmd.ApplyNodePresetDefaults(cfg.Runtime)
	if cfg.Runtime.Name == "" {
		cfg.Runtime.Name = "runtime-sandbox"
	}
	if cfg.Runtime.Addr == "" {
		cfg.Runtime.Addr = ":8083"
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
