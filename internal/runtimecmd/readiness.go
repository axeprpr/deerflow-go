package runtimecmd

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func (c NodeConfig) ReadyProbe(spec harnessruntime.RuntimeNodeLaunchSpec) commandrun.ReadyFunc {
	targets := []string{}
	if spec.ServesRemoteWorker {
		targets = append(targets, commandrun.HTTPURL(spec.RemoteWorkerAddr)+harnessruntime.DefaultRemoteWorkerHealthPath)
	}
	if spec.ServesRemoteSandbox {
		targets = append(targets, commandrun.HTTPURL(spec.RemoteSandboxAddr)+harnessruntime.DefaultRemoteSandboxHealthPath)
	}
	if spec.ServesRemoteState {
		targets = append(targets, commandrun.HTTPURL(spec.RemoteStateAddr)+harnessruntime.DefaultRemoteStateHealthPath)
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
