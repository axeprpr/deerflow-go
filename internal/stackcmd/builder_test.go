package stackcmd

import (
	"context"
	"testing"
)

func TestBuilderCarriesResolvedDeploymentSpec(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	builder := NewBuilder(cfg)
	if builder.Config().Gateway.Runtime.StateStoreURL == "" {
		t.Fatal("builder.Config().Gateway.Runtime.StateStoreURL = empty")
	}
	spec := builder.DeploymentSpec()
	if len(spec.Components) < 3 {
		t.Fatalf("components = %d", len(spec.Components))
	}
	if spec.Components[2].Kind != ComponentState {
		t.Fatalf("components[2].Kind = %q", spec.Components[2].Kind)
	}
	if spec.Components[2].Addr != ":19082" {
		t.Fatalf("state component addr = %q", spec.Components[2].Addr)
	}
}

func TestBuilderBuildLauncherUsesResolvedSpec(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Worker.Addr = ":19081"

	builder := NewBuilder(cfg)
	launcher, err := builder.BuildLauncher(context.Background())
	if err != nil {
		t.Fatalf("BuildLauncher() error = %v", err)
	}
	if launcher.DeploymentSpec().WorkerDispatch == "" {
		t.Fatal("launcher.DeploymentSpec().WorkerDispatch = empty")
	}
}
