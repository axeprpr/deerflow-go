package stackcmd

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func (c Config) ReadyProbe() commandrun.ReadyFunc {
	cfg := c.withDefaults()
	targets := []string{
		commandrun.HTTPURL(cfg.Gateway.Addr) + "/health",
		commandrun.HTTPURL(cfg.Worker.Addr) + harnessruntime.DefaultRemoteWorkerHealthPath,
	}
	if cfg.usesDedicatedStateService() {
		targets = append(targets, commandrun.HTTPURL(cfg.Sandbox.Runtime.Addr)+harnessruntime.DefaultRemoteSandboxHealthPath)
	} else if cfg.Worker.SandboxBackend != harnessruntime.SandboxBackendRemote {
		targets = append(targets, commandrun.HTTPURL(cfg.Worker.Addr)+harnessruntime.DefaultRemoteSandboxHealthPath)
	}
	if cfg.usesDedicatedStateService() {
		targets = append(targets, commandrun.HTTPURL(cfg.State.Runtime.Addr)+harnessruntime.DefaultRemoteStateHealthPath)
	} else {
		targets = append(targets, commandrun.HTTPURL(cfg.Worker.Addr)+harnessruntime.DefaultRemoteStateHealthPath)
	}
	filtered := make([]string, 0, len(targets))
	for _, target := range targets {
		if trimmed := strings.TrimSpace(target); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return commandrun.HTTPReadyProbe{
		Interval: 50 * time.Millisecond,
		Targets:  filtered,
	}.Wait
}
