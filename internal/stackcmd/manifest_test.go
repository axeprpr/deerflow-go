package stackcmd

import (
	"reflect"
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
	if len(manifest.Processes) != 4 {
		t.Fatalf("processes = %d", len(manifest.Processes))
	}
	if manifest.Processes[0].Binary != "langgraph" {
		t.Fatalf("gateway process = %+v", manifest.Processes[0])
	}
	if manifest.Processes[1].Binary != "runtime-node" {
		t.Fatalf("worker process = %+v", manifest.Processes[1])
	}
	processes := processManifestByName(manifest.Processes)
	if processes["gateway"].Component != ComponentGateway {
		t.Fatalf("gateway component = %q", processes["gateway"].Component)
	}
	if processes["worker"].Component != ComponentWorker {
		t.Fatalf("worker component = %q", processes["worker"].Component)
	}
	if !reflect.DeepEqual(processes["gateway"].DependsOn, []string{"worker"}) {
		t.Fatalf("gateway depends_on = %#v", processes["gateway"].DependsOn)
	}
	if !reflect.DeepEqual(processes["worker"].DependsOn, []string{"state", "sandbox"}) {
		t.Fatalf("worker depends_on = %#v", processes["worker"].DependsOn)
	}
	if processes["state"].ReadyURL == "" || processes["sandbox"].ReadyURL == "" {
		t.Fatalf("state/sandbox ready url = %q / %q", processes["state"].ReadyURL, processes["sandbox"].ReadyURL)
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

func TestManifestIncludesSandboxMaxActiveLeases(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"
	cfg.Worker.SandboxMaxActiveLeases = 6

	manifest := cfg.Manifest()
	processes := processManifestByName(manifest.Processes)
	if !strings.Contains(strings.Join(processes["sandbox"].Args, " "), "-max-active-leases=6") {
		t.Fatalf("sandbox args = %#v", processes["sandbox"].Args)
	}
	joined := strings.Join(manifest.StartupLines(langgraphcmd.BuildInfo{}, false, ""), "\n")
	if !strings.Contains(joined, "max_active_leases=6") {
		t.Fatalf("StartupLines = %q", joined)
	}
}

func TestManifestStateAndSandboxArgsMatchCommandFlags(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	manifest := cfg.Manifest()
	processes := processManifestByName(manifest.Processes)
	stateArgs := strings.Join(processes["state"].Args, " ")
	sandboxArgs := strings.Join(processes["sandbox"].Args, " ")
	if strings.Contains(stateArgs, "-role=") || strings.Contains(stateArgs, "-transport-backend=") {
		t.Fatalf("state args include unsupported runtime-node flags: %q", stateArgs)
	}
	if strings.Contains(sandboxArgs, "-role=") || strings.Contains(sandboxArgs, "-state-provider=") {
		t.Fatalf("sandbox args include unsupported runtime-node flags: %q", sandboxArgs)
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

func TestManifestProcessDependenciesForSharedSQLitePreset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedSQLite
	cfg.Worker.Addr = ":19081"

	manifest := cfg.Manifest()
	if len(manifest.Processes) != 2 {
		t.Fatalf("processes = %d", len(manifest.Processes))
	}
	processes := processManifestByName(manifest.Processes)
	if !reflect.DeepEqual(processes["gateway"].DependsOn, []string{"worker"}) {
		t.Fatalf("gateway depends_on = %#v", processes["gateway"].DependsOn)
	}
	if len(processes["worker"].DependsOn) != 0 {
		t.Fatalf("worker depends_on = %#v", processes["worker"].DependsOn)
	}
}

func TestManifestValidateProcessGraphDedicatedRemotePreset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	if err := cfg.Manifest().ValidateProcessGraph(); err != nil {
		t.Fatalf("ValidateProcessGraph() error = %v", err)
	}
}

func TestManifestValidateProcessGraphRejectsUnknownDependency(t *testing.T) {
	manifest := StackManifest{
		Processes: []ProcessManifest{
			{Name: "gateway", DependsOn: []string{"worker"}},
		},
	}
	err := manifest.ValidateProcessGraph()
	if err == nil || !strings.Contains(err.Error(), "unknown process") {
		t.Fatalf("ValidateProcessGraph() error = %v, want unknown process", err)
	}
}

func TestManifestValidateProcessGraphRejectsCycle(t *testing.T) {
	manifest := StackManifest{
		Processes: []ProcessManifest{
			{Name: "gateway", DependsOn: []string{"worker"}},
			{Name: "worker", DependsOn: []string{"gateway"}},
		},
	}
	err := manifest.ValidateProcessGraph()
	if err == nil || !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("ValidateProcessGraph() error = %v, want cycle detected", err)
	}
}

func processManifestByName(processes []ProcessManifest) map[string]ProcessManifest {
	index := make(map[string]ProcessManifest, len(processes))
	for _, process := range processes {
		index[process.Name] = process
	}
	return index
}
