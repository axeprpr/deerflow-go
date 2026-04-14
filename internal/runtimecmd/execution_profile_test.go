package runtimecmd

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDefaultExecutionProfileForGatewayUsesRemoteTransport(t *testing.T) {
	profile := DefaultExecutionProfile(RuntimeNodePresetSharedSQLite, harnessruntime.RuntimeNodeRoleGateway)
	if profile.Transport != harnessruntime.WorkerTransportBackendRemote {
		t.Fatalf("Transport = %q", profile.Transport)
	}
	if profile.Sandbox != harnessruntime.SandboxBackendLocalLinux {
		t.Fatalf("Sandbox = %q", profile.Sandbox)
	}
}

func TestDefaultExecutionProfileForWorkerUsesQueueTransport(t *testing.T) {
	profile := DefaultExecutionProfile(RuntimeNodePresetSharedRemote, harnessruntime.RuntimeNodeRoleWorker)
	if profile.Transport != harnessruntime.WorkerTransportBackendQueue {
		t.Fatalf("Transport = %q", profile.Transport)
	}
}

func TestExecutionProfileApplyLeavesExplicitValues(t *testing.T) {
	cfg := NodeConfig{
		TransportBackend: harnessruntime.WorkerTransportBackendDirect,
		SandboxBackend:   harnessruntime.SandboxBackendRemote,
	}
	cfg = DefaultExecutionProfile(RuntimeNodePresetSharedSQLite, harnessruntime.RuntimeNodeRoleWorker).Apply(cfg)
	if cfg.TransportBackend != harnessruntime.WorkerTransportBackendDirect {
		t.Fatalf("TransportBackend = %q", cfg.TransportBackend)
	}
	if cfg.SandboxBackend != harnessruntime.SandboxBackendRemote {
		t.Fatalf("SandboxBackend = %q", cfg.SandboxBackend)
	}
}
