package stackcmd

import (
	"testing"

	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
)

func TestDefaultPresetSpecForSharedRemote(t *testing.T) {
	spec := DefaultPresetSpec(StackPresetSharedRemote)
	if spec.GatewayRuntimePreset != runtimecmd.RuntimeNodePresetSharedRemote {
		t.Fatalf("gateway preset = %q", spec.GatewayRuntimePreset)
	}
	if spec.WorkerRuntimePreset != runtimecmd.RuntimeNodePresetSharedSQLite {
		t.Fatalf("worker preset = %q", spec.WorkerRuntimePreset)
	}
	if !spec.DedicatedStateService {
		t.Fatal("DedicatedStateService = false")
	}
	if !spec.DedicatedSandboxService {
		t.Fatal("DedicatedSandboxService = false")
	}
}

func TestBuildPresetConfigUsesRequestedPreset(t *testing.T) {
	cfg := BuildPresetConfig(StackPresetSharedRemote)
	if cfg.Preset != StackPresetSharedRemote {
		t.Fatalf("preset = %q", cfg.Preset)
	}
	if cfg.Gateway.Runtime.Preset != runtimecmd.RuntimeNodePresetSharedRemote {
		t.Fatalf("gateway runtime preset = %q", cfg.Gateway.Runtime.Preset)
	}
	if cfg.Worker.Preset != runtimecmd.RuntimeNodePresetSharedSQLite {
		t.Fatalf("worker preset = %q", cfg.Worker.Preset)
	}
}
