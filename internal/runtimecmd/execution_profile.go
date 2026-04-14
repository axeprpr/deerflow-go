package runtimecmd

import "github.com/axeprpr/deerflow-go/pkg/harnessruntime"

type ExecutionProfile struct {
	Name      string
	Transport harnessruntime.WorkerTransportBackend
	Sandbox   harnessruntime.SandboxBackend
}

func DefaultExecutionProfile(preset RuntimeNodePreset, role harnessruntime.RuntimeNodeRole) ExecutionProfile {
	profile := ExecutionProfile{
		Name:      "queue-local",
		Transport: harnessruntime.WorkerTransportBackendQueue,
		Sandbox:   harnessruntime.SandboxBackendLocalLinux,
	}
	switch role {
	case harnessruntime.RuntimeNodeRoleGateway:
		profile.Name = "remote-dispatch"
		profile.Transport = harnessruntime.WorkerTransportBackendRemote
	}
	switch preset {
	case RuntimeNodePresetFastLocal:
		profile.Name = "fast-local"
	case RuntimeNodePresetSharedRemote:
		if role == harnessruntime.RuntimeNodeRoleGateway {
			profile.Name = "shared-remote-gateway"
			profile.Transport = harnessruntime.WorkerTransportBackendRemote
		} else {
			profile.Name = "shared-remote-worker"
		}
	case RuntimeNodePresetSharedSQLite:
		if role == harnessruntime.RuntimeNodeRoleGateway {
			profile.Name = "shared-sqlite-gateway"
			profile.Transport = harnessruntime.WorkerTransportBackendRemote
		} else {
			profile.Name = "shared-sqlite-worker"
		}
	}
	return profile
}

func (p ExecutionProfile) Apply(config NodeConfig) NodeConfig {
	if config.TransportBackend == "" {
		config.TransportBackend = p.Transport
	}
	if config.SandboxBackend == "" {
		config.SandboxBackend = p.Sandbox
	}
	return config
}
