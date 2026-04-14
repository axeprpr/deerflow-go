package stackcmd

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDefaultStackProfileForSharedRemote(t *testing.T) {
	profile := DefaultStackProfile(StackPresetSharedRemote)
	if profile.Name != "shared-remote" {
		t.Fatalf("Name = %q", profile.Name)
	}
	if profile.GatewayStateProvider != harnessruntime.RuntimeStateProviderModeAuto {
		t.Fatalf("GatewayStateProvider = %q", profile.GatewayStateProvider)
	}
	if profile.WorkerStateProvider != harnessruntime.RuntimeStateProviderModeAuto {
		t.Fatalf("WorkerStateProvider = %q", profile.WorkerStateProvider)
	}
	if !profile.Preset.DedicatedStateService {
		t.Fatal("DedicatedStateService = false")
	}
}

func TestDefaultStackProfileConfigUsesSharedSQLiteDefaults(t *testing.T) {
	cfg := DefaultStackProfileConfig(StackPresetSharedSQLite)
	if cfg.Gateway.Runtime.StateProvider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("gateway state provider = %q", cfg.Gateway.Runtime.StateProvider)
	}
	if cfg.Worker.StateProvider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("worker state provider = %q", cfg.Worker.StateProvider)
	}
	if cfg.Worker.TransportBackend != harnessruntime.WorkerTransportBackendQueue {
		t.Fatalf("worker transport = %q", cfg.Worker.TransportBackend)
	}
}
