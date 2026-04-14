package runtimecmd

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDefaultStateProfileForSharedRemoteGateway(t *testing.T) {
	profile := DefaultStateProfile(RuntimeNodePresetSharedRemote, harnessruntime.RuntimeNodeRoleGateway)
	if profile.Name != "shared-remote" {
		t.Fatalf("Name = %q", profile.Name)
	}
	if profile.Provider != harnessruntime.RuntimeStateProviderModeAuto {
		t.Fatalf("Provider = %q", profile.Provider)
	}
}

func TestDefaultStateProfileForSharedRemoteWorkerFallsBackToSharedSQLite(t *testing.T) {
	profile := DefaultStateProfile(RuntimeNodePresetSharedRemote, harnessruntime.RuntimeNodeRoleWorker)
	if profile.Name != "shared-sqlite" {
		t.Fatalf("Name = %q", profile.Name)
	}
	if profile.Provider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("Provider = %q", profile.Provider)
	}
}

func TestStateProfileApplyAssignsProviderDefaults(t *testing.T) {
	cfg := NodeConfig{
		Role:   harnessruntime.RuntimeNodeRoleWorker,
		Preset: RuntimeNodePresetFastLocal,
	}
	cfg = DefaultStateProfile(cfg.Preset, cfg.Role).Apply(cfg)
	if cfg.StateProvider != harnessruntime.RuntimeStateProviderModeIsolated {
		t.Fatalf("StateProvider = %q", cfg.StateProvider)
	}
}
