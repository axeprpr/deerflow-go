package stackcmd

import (
	"flag"
	"strings"
	"testing"

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

func TestPrepareCommandSpawnProcessesUsesProcessLauncher(t *testing.T) {
	binDir := installTestProcessBinaries(t, "langgraph", "runtime-node", "runtime-state", "runtime-sandbox")
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack-spawn", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-spawn-processes",
			"-process-binary-dir=" + binDir,
			"-spawn-restart-policy=always",
			"-spawn-max-restarts=5",
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
	if !strings.Contains(strings.Join(prepared.StartupLines, "\n"), "launch_mode=external-processes") {
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
