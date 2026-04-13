package stackcmd

import (
	"fmt"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type LaunchSpec struct {
	Preset                StackPreset
	GatewayAddr           string
	WorkerAddr            string
	StateAddr             string
	SandboxAddr           string
	DedicatedStateService bool
	DedicatedSandbox      bool
}

func (c Config) LaunchSpec() LaunchSpec {
	cfg := c.withDefaults()
	spec := LaunchSpec{
		Preset:                cfg.effectivePreset(),
		GatewayAddr:           cfg.Gateway.Addr,
		WorkerAddr:            cfg.Worker.Addr,
		DedicatedStateService: cfg.usesDedicatedStateService(),
		DedicatedSandbox:      cfg.usesDedicatedStateService(),
	}
	if spec.DedicatedStateService {
		spec.StateAddr = cfg.State.Runtime.Addr
	}
	if spec.DedicatedSandbox {
		spec.SandboxAddr = cfg.Sandbox.Runtime.Addr
	}
	return spec
}

func (s LaunchSpec) GatewayHealthURL() string {
	return commandrun.HTTPURL(s.GatewayAddr) + "/health"
}

func (s LaunchSpec) WorkerHealthURL() string {
	return commandrun.HTTPURL(s.WorkerAddr) + harnessruntime.DefaultRemoteWorkerHealthPath
}

func (s LaunchSpec) WorkerDispatchURL() string {
	return commandrun.HTTPURL(s.WorkerAddr) + harnessruntime.DefaultRemoteWorkerDispatchPath
}

func (s LaunchSpec) WorkerStateHealthURL() string {
	if s.DedicatedStateService {
		return commandrun.HTTPURL(s.StateAddr) + harnessruntime.DefaultRemoteStateHealthPath
	}
	return commandrun.HTTPURL(s.WorkerAddr) + harnessruntime.DefaultRemoteStateHealthPath
}

func (s LaunchSpec) WorkerSandboxHealthURL() string {
	if s.DedicatedSandbox {
		return commandrun.HTTPURL(s.SandboxAddr) + harnessruntime.DefaultRemoteSandboxHealthPath
	}
	return commandrun.HTTPURL(s.WorkerAddr) + harnessruntime.DefaultRemoteSandboxHealthPath
}

func (s LaunchSpec) ReadyLines() []string {
	lines := []string{
		fmt.Sprintf("  Worker server: %s", s.WorkerDispatchURL()),
	}
	if s.DedicatedSandbox {
		lines = append(lines, fmt.Sprintf("  Sandbox server: %s", s.WorkerSandboxHealthURL()))
	} else {
		lines = append(lines, fmt.Sprintf("  Worker sandbox: %s", s.WorkerSandboxHealthURL()))
	}
	if s.DedicatedStateService {
		lines = append(lines, fmt.Sprintf("  State server: %s", s.WorkerStateHealthURL()))
	} else {
		lines = append(lines, fmt.Sprintf("  Worker state: %s", s.WorkerStateHealthURL()))
	}
	return lines
}

func (s LaunchSpec) ReadyTargets() []string {
	targets := []string{
		s.GatewayHealthURL(),
		s.WorkerHealthURL(),
		s.WorkerSandboxHealthURL(),
		s.WorkerStateHealthURL(),
	}
	return targets
}
