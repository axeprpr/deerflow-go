package langgraphcmd

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func (c Config) ReadyProbe() commandrun.ReadyFunc {
	targets := []string{
		commandrun.HTTPURL(c.Addr) + "/health",
	}
	if c.Runtime.Role == harnessruntime.RuntimeNodeRoleAllInOne {
		workerBase := commandrun.HTTPURL(c.Runtime.Addr)
		targets = append(targets, workerBase+harnessruntime.DefaultRemoteWorkerHealthPath)
		if c.Runtime.SandboxBackend != harnessruntime.SandboxBackendRemote {
			targets = append(targets, workerBase+harnessruntime.DefaultRemoteSandboxHealthPath)
		}
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
