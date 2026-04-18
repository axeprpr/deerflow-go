package stackcmd

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestPrepareCommandBuildsSplitReadyLines(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args:   []string{"-addr=:18080", "-worker-addr=:19081"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if len(prepared.ReadyLines) < 3 {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
	joined := strings.Join(prepared.ReadyLines, "\n")
	if !strings.Contains(joined, ":19081") {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
	if !strings.Contains(joined, harnessruntime.DefaultRemoteStateHealthPath) {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
	if prepared.Ready == nil {
		t.Fatal("PrepareCommand().Ready = nil")
	}
}

func TestPrepareCommandAcceptsSharedBackendFlags(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-state-backend=sqlite",
			"-state-store=sqlite:///tmp/runtime.sqlite3",
			"-snapshot-backend=sqlite",
			"-event-backend=sqlite",
			"-thread-backend=sqlite",
			"-worker-transport=queue",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if !strings.Contains(strings.Join(prepared.StartupLines, "\n"), "store=sqlite:///tmp/runtime.sqlite3") {
		t.Fatalf("StartupLines = %#v", prepared.StartupLines)
	}
	if !strings.Contains(strings.Join(prepared.StartupLines, "\n"), "state_provider=shared-sqlite") {
		t.Fatalf("StartupLines = %#v", prepared.StartupLines)
	}
}

func TestPrepareCommandAcceptsRemoteStateBackendFlags(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-state-backend=remote",
			"-snapshot-backend=remote",
			"-event-backend=remote",
			"-thread-backend=remote",
			"-worker-addr=:19081",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	joined := strings.Join(prepared.ReadyLines, "\n")
	if !strings.Contains(joined, harnessruntime.DefaultRemoteStateHealthPath) {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
}

func TestPrepareCommandAcceptsSharedRemotePreset(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-preset=shared-remote",
			"-worker-addr=:19081",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if !strings.Contains(strings.Join(prepared.StartupLines, "\n"), "preset=shared-remote") {
		t.Fatalf("StartupLines = %#v", prepared.StartupLines)
	}
	joined := strings.Join(prepared.ReadyLines, "\n")
	if !strings.Contains(joined, "127.0.0.1:19082") {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
	if !strings.Contains(joined, harnessruntime.DefaultRemoteStateHealthPath) {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
	if !strings.Contains(joined, "127.0.0.1:19083") {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
}

func TestPrepareCommandPrintManifest(t *testing.T) {
	var stdout strings.Builder
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack-manifest", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stdout: &stdout,
		Args:   []string{"-print-manifest", "-preset=shared-remote", "-worker-addr=:19081"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil || prepared.RunFunc == nil {
		t.Fatal("PrepareCommand() did not build manifest run func")
	}
	if err := prepared.Run(); err != nil {
		t.Fatalf("prepared.Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"preset\": \"shared-remote\"") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"worker_dispatch\"") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPrepareCommandPrintManifestIncludesSandboxMaxActiveLeases(t *testing.T) {
	var stdout strings.Builder
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack-manifest-sandbox-leases", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stdout: &stdout,
		Args: []string{
			"-print-manifest",
			"-preset=shared-remote",
			"-worker-addr=:19081",
			"-sandbox-max-active-leases=9",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if err := prepared.Run(); err != nil {
		t.Fatalf("prepared.Run() error = %v", err)
	}
	var manifest StackManifest
	if err := json.Unmarshal([]byte(stdout.String()), &manifest); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := manifestSandboxMaxActiveLeases(manifest); got != 9 {
		t.Fatalf("sandbox max active leases = %d, want 9", got)
	}
}

func TestPrepareCommandPrintManifestSandboxServiceLeaseOverride(t *testing.T) {
	var stdout strings.Builder
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack-manifest-sandbox-override", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stdout: &stdout,
		Args: []string{
			"-print-manifest",
			"-preset=shared-remote",
			"-worker-addr=:19081",
			"-sandbox-max-active-leases=9",
			"-sandbox-service-max-active-leases=4",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if err := prepared.Run(); err != nil {
		t.Fatalf("prepared.Run() error = %v", err)
	}
	var manifest StackManifest
	if err := json.Unmarshal([]byte(stdout.String()), &manifest); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := manifestSandboxMaxActiveLeases(manifest); got != 4 {
		t.Fatalf("sandbox max active leases = %d, want 4", got)
	}
}

func TestPrepareCommandSpawnProcessesUsesProcessLauncher(t *testing.T) {
	binDir := installTestProcessBinaries(t, "langgraph", "runtime-node", "runtime-state", "runtime-sandbox")
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack-spawn", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-spawn-processes",
			"-process-binary-dir=" + binDir,
			"-spawn-restart-policy=always",
			"-spawn-max-restarts=5",
			"-spawn-failure-isolation",
			"-preset=shared-remote",
			"-worker-addr=:19081",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if _, ok := prepared.Lifecycle.(*ProcessLauncher); !ok {
		t.Fatalf("Lifecycle type = %T, want *ProcessLauncher", prepared.Lifecycle)
	}
	launcher := prepared.Lifecycle.(*ProcessLauncher)
	if len(launcher.lifecycles) == 0 {
		t.Fatal("spawn launcher lifecycles = 0")
	}
	if launcher.lifecycles[0].restartPolicy != ProcessRestartAlways {
		t.Fatalf("restart policy = %q", launcher.lifecycles[0].restartPolicy)
	}
	if launcher.lifecycles[0].maxRestarts != 5 {
		t.Fatalf("max restarts = %d", launcher.lifecycles[0].maxRestarts)
	}
	if !launcher.failureIsolation {
		t.Fatal("failure isolation = false, want true")
	}
	if !strings.Contains(strings.Join(prepared.StartupLines, "\n"), "launch_mode=external-processes") {
		t.Fatalf("StartupLines = %#v", prepared.StartupLines)
	}
	if !strings.Contains(strings.Join(prepared.StartupLines, "\n"), "failure_isolation=enabled") {
		t.Fatalf("StartupLines = %#v", prepared.StartupLines)
	}
}

func TestPrepareCommandSpawnProcessesRejectsInvalidRestartPolicy(t *testing.T) {
	binDir := installTestProcessBinaries(t, "langgraph", "runtime-node")
	_, err := PrepareCommand(flag.NewFlagSet("runtime-stack-spawn-invalid-policy", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-spawn-processes",
			"-process-binary-dir=" + binDir,
			"-spawn-restart-policy=sometimes",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid restart policy") {
		t.Fatalf("PrepareCommand() error = %v, want invalid restart policy", err)
	}
}

func TestPrepareCommandWriteBundleRejectsInvalidRestartPolicy(t *testing.T) {
	_, err := PrepareCommand(flag.NewFlagSet("runtime-stack-bundle-invalid-policy", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Args: []string{
			"-write-bundle=" + t.TempDir(),
			"-bundle-restart-policy=sometimes",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid restart policy") {
		t.Fatalf("PrepareCommand() error = %v, want invalid restart policy", err)
	}
}

func TestPrepareCommandSpawnBundleUsesProcessLauncher(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"
	dir := t.TempDir()
	if err := WriteBundle(dir, cfg.Manifest()); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}
	binDir := installTestProcessBinaries(t, "langgraph", "runtime-node", "runtime-state", "runtime-sandbox")

	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack-spawn-bundle", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-spawn-bundle=" + dir,
			"-process-binary-dir=" + binDir,
			"-spawn-restart-policy=always",
			"-spawn-max-restarts=2",
			"-spawn-failure-isolation",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	launcher, ok := prepared.Lifecycle.(*ProcessLauncher)
	if !ok {
		t.Fatalf("Lifecycle type = %T, want *ProcessLauncher", prepared.Lifecycle)
	}
	if len(launcher.lifecycles) != 4 {
		t.Fatalf("spawn bundle lifecycles = %d, want 4", len(launcher.lifecycles))
	}
	if launcher.lifecycles[0].restartPolicy != ProcessRestartAlways {
		t.Fatalf("restart policy = %q", launcher.lifecycles[0].restartPolicy)
	}
	if launcher.lifecycles[0].maxRestarts != 2 {
		t.Fatalf("max restarts = %d", launcher.lifecycles[0].maxRestarts)
	}
	if !launcher.failureIsolation {
		t.Fatal("failure isolation = false, want true")
	}
	joined := strings.Join(prepared.StartupLines, "\n")
	if !strings.Contains(joined, "launch_mode=external-processes-bundle") {
		t.Fatalf("StartupLines = %#v", prepared.StartupLines)
	}
	if !strings.Contains(joined, dir) {
		t.Fatalf("StartupLines = %#v", prepared.StartupLines)
	}
}

func TestPrepareCommandSpawnBundleRejectsInvalidBundle(t *testing.T) {
	binDir := installTestProcessBinaries(t, "langgraph", "runtime-node")
	_, err := PrepareCommand(flag.NewFlagSet("runtime-stack-spawn-bundle-invalid", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-spawn-bundle=" + filepath.Join(t.TempDir(), "missing"),
			"-process-binary-dir=" + binDir,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "stack-manifest.json") {
		t.Fatalf("PrepareCommand() error = %v, want missing stack-manifest", err)
	}
}

func TestPrepareCommandSpawnBundleUsesHostPlanPolicy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"
	dir := t.TempDir()
	if err := WriteBundleWithOptions(dir, cfg.Manifest(), BundleOptions{
		RestartPolicy:     ProcessRestartAlways,
		MaxRestarts:       9,
		RestartDelay:      2 * time.Second,
		DependencyTimeout: 45 * time.Second,
		FailureIsolation:  true,
	}); err != nil {
		t.Fatalf("WriteBundleWithOptions() error = %v", err)
	}
	hostPlan, err := LoadBundleHostPlan(dir)
	if err != nil {
		t.Fatalf("LoadBundleHostPlan() error = %v", err)
	}
	for i := range hostPlan.Processes {
		if strings.TrimSpace(hostPlan.Processes[i].Name) != "worker" {
			continue
		}
		hostPlan.Processes[i].RestartPolicy = string(ProcessRestartNever)
		hostPlan.Processes[i].MaxRestarts = 1
		hostPlan.Processes[i].RestartDelayMilli = 250
		hostPlan.Processes[i].DependencyTimeoutMilli = 12000
	}
	data, err := json.MarshalIndent(hostPlan, "", "  ")
	if err != nil {
		t.Fatalf("Marshal(hostPlan) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "host-plan.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(host-plan.json) error = %v", err)
	}
	binDir := installTestProcessBinaries(t, "langgraph", "runtime-node", "runtime-state", "runtime-sandbox")

	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack-spawn-bundle-host-plan", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-spawn-bundle=" + dir,
			"-spawn-bundle-use-host-plan",
			"-process-binary-dir=" + binDir,
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	launcher, ok := prepared.Lifecycle.(*ProcessLauncher)
	if !ok {
		t.Fatalf("Lifecycle type = %T, want *ProcessLauncher", prepared.Lifecycle)
	}
	if len(launcher.lifecycles) == 0 {
		t.Fatal("spawn bundle lifecycles = 0")
	}
	if launcher.lifecycles[0].restartPolicy != ProcessRestartAlways {
		t.Fatalf("restart policy = %q", launcher.lifecycles[0].restartPolicy)
	}
	if launcher.lifecycles[0].maxRestarts != 9 {
		t.Fatalf("max restarts = %d", launcher.lifecycles[0].maxRestarts)
	}
	if launcher.lifecycles[0].restartDelay != 2*time.Second {
		t.Fatalf("restart delay = %s", launcher.lifecycles[0].restartDelay)
	}
	if launcher.lifecycles[0].dependencyTimeout != 45*time.Second {
		t.Fatalf("dependency timeout = %s", launcher.lifecycles[0].dependencyTimeout)
	}
	worker := lifecycleByName(launcher, "worker")
	if worker == nil {
		t.Fatal("worker lifecycle = nil")
	}
	if worker.restartPolicy != ProcessRestartNever {
		t.Fatalf("worker restart policy = %q", worker.restartPolicy)
	}
	if worker.maxRestarts != 1 {
		t.Fatalf("worker max restarts = %d", worker.maxRestarts)
	}
	if worker.restartDelay != 250*time.Millisecond {
		t.Fatalf("worker restart delay = %s", worker.restartDelay)
	}
	if worker.dependencyTimeout != 12*time.Second {
		t.Fatalf("worker dependency timeout = %s", worker.dependencyTimeout)
	}
	if !launcher.failureIsolation {
		t.Fatal("failure isolation = false, want true")
	}
	if !strings.Contains(strings.Join(prepared.StartupLines, "\n"), "policy_source=host-plan") {
		t.Fatalf("StartupLines = %#v", prepared.StartupLines)
	}
}

func manifestSandboxMaxActiveLeases(manifest StackManifest) int {
	for _, component := range manifest.Components {
		if component.Kind == ComponentSandbox {
			return component.Node.SandboxProfile.Limits.MaxActiveLeases
		}
	}
	return 0
}

func lifecycleByName(launcher *ProcessLauncher, name string) *processLifecycle {
	if launcher == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	for _, lifecycle := range launcher.lifecycles {
		if lifecycle == nil {
			continue
		}
		if strings.TrimSpace(lifecycle.name) == name {
			return lifecycle
		}
	}
	return nil
}
