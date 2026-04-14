package stackcmd

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

func TestManifestIncludesDedicatedRemoteComponents(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	manifest := cfg.Manifest()
	if manifest.Preset != StackPresetSharedRemote {
		t.Fatalf("Preset = %q", manifest.Preset)
	}
	if manifest.WorkerDispatch == "" {
		t.Fatal("WorkerDispatch = empty")
	}
	if len(manifest.Components) != 4 {
		t.Fatalf("components = %d", len(manifest.Components))
	}
	if !manifest.Components[2].Dedicated || manifest.Components[2].Kind != ComponentState {
		t.Fatalf("state component = %+v", manifest.Components[2])
	}
	if !manifest.Components[3].Dedicated || manifest.Components[3].Kind != ComponentSandbox {
		t.Fatalf("sandbox component = %+v", manifest.Components[3])
	}
}

func TestManifestReadyLinesUseDedicatedLabels(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	joined := strings.Join(cfg.Manifest().ReadyLines(), "\n")
	if !strings.Contains(joined, "State server:") {
		t.Fatalf("ReadyLines = %q", joined)
	}
	if !strings.Contains(joined, "Sandbox server:") {
		t.Fatalf("ReadyLines = %q", joined)
	}
}

func TestManifestStartupLinesIncludeComponentProfiles(t *testing.T) {
	cfg := DefaultConfig()
	lines := cfg.Manifest().StartupLines(langgraphcmd.BuildInfo{}, false, "")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "preset=") {
		t.Fatalf("StartupLines = %q", joined)
	}
	if !strings.Contains(joined, "profile=") {
		t.Fatalf("StartupLines = %q", joined)
	}
	if !strings.Contains(joined, "worker_dispatch=") {
		t.Fatalf("StartupLines = %q", joined)
	}
}
