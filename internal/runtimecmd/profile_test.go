package runtimecmd

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDefaultRuntimeProfileForGatewaySharedRemote(t *testing.T) {
	profile := DefaultRuntimeProfile(RuntimeNodePresetSharedRemote, harnessruntime.RuntimeNodeRoleGateway)
	if profile.State.Name != "shared-remote" {
		t.Fatalf("state profile = %q", profile.State.Name)
	}
	if profile.Execution.Transport != harnessruntime.WorkerTransportBackendRemote {
		t.Fatalf("transport = %q", profile.Execution.Transport)
	}
}

func TestRuntimeProfileApplySetsStateAndExecutionDefaults(t *testing.T) {
	cfg := NodeConfig{
		Role:   harnessruntime.RuntimeNodeRoleWorker,
		Preset: RuntimeNodePresetFastLocal,
	}
	cfg = DefaultRuntimeProfile(cfg.Preset, cfg.Role).Apply(cfg)
	if cfg.StateProvider != harnessruntime.RuntimeStateProviderModeIsolated {
		t.Fatalf("StateProvider = %q", cfg.StateProvider)
	}
	if cfg.TransportBackend != harnessruntime.WorkerTransportBackendQueue {
		t.Fatalf("TransportBackend = %q", cfg.TransportBackend)
	}
	if cfg.SandboxBackend != harnessruntime.SandboxBackendLocalLinux {
		t.Fatalf("SandboxBackend = %q", cfg.SandboxBackend)
	}
}
