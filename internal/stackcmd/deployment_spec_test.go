package stackcmd

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDeploymentSpecUsesDedicatedComponentsForSharedRemotePreset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	spec := cfg.DeploymentSpec()
	if spec.Preset != StackPresetSharedRemote {
		t.Fatalf("Preset = %q", spec.Preset)
	}
	if spec.WorkerDispatch != "http://127.0.0.1:19081"+harnessruntime.DefaultRemoteWorkerDispatchPath {
		t.Fatalf("WorkerDispatch = %q", spec.WorkerDispatch)
	}
	if len(spec.Components) != 4 {
		t.Fatalf("components = %d", len(spec.Components))
	}
	if spec.Components[2].Addr != ":19082" {
		t.Fatalf("state addr = %q", spec.Components[2].Addr)
	}
	if spec.Components[3].Addr != ":19083" {
		t.Fatalf("sandbox addr = %q", spec.Components[3].Addr)
	}
}

func TestLauncherCarriesDeploymentSpec(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Worker.Addr = ":19081"

	launcher, err := cfg.BuildLauncher(context.Background())
	if err != nil {
		t.Fatalf("BuildLauncher() error = %v", err)
	}
	if launcher.DeploymentSpec().WorkerDispatch == "" {
		t.Fatal("DeploymentSpec().WorkerDispatch = empty")
	}
}
