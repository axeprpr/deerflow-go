package stackcmd

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestLaunchSpecUsesDedicatedServicesForSharedRemotePreset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	spec := cfg.LaunchSpec()
	if !spec.DedicatedStateService {
		t.Fatal("DedicatedStateService = false")
	}
	if !spec.DedicatedSandbox {
		t.Fatal("DedicatedSandbox = false")
	}
	if spec.WorkerStateHealthURL() != "http://127.0.0.1:19082"+harnessruntime.DefaultRemoteStateHealthPath {
		t.Fatalf("WorkerStateHealthURL() = %q", spec.WorkerStateHealthURL())
	}
	if spec.WorkerSandboxHealthURL() != "http://127.0.0.1:19083"+harnessruntime.DefaultRemoteSandboxHealthPath {
		t.Fatalf("WorkerSandboxHealthURL() = %q", spec.WorkerSandboxHealthURL())
	}
}
