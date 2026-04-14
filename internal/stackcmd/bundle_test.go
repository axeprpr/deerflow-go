package stackcmd

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

func TestWriteBundleWritesManifestAndProcesses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	dir := t.TempDir()
	if err := WriteBundle(dir, cfg.Manifest()); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stack-manifest.json")); err != nil {
		t.Fatalf("manifest stat error = %v", err)
	}
	hostPlanData, err := os.ReadFile(filepath.Join(dir, "host-plan.json"))
	if err != nil {
		t.Fatalf("host plan read error = %v", err)
	}
	if !strings.Contains(string(hostPlanData), "\"systemd\"") || !strings.Contains(string(hostPlanData), "\"electron\"") {
		t.Fatalf("host plan = %q", string(hostPlanData))
	}
	var hostPlan HostPlan
	if err := json.Unmarshal(hostPlanData, &hostPlan); err != nil {
		t.Fatalf("host plan decode error = %v", err)
	}
	if len(hostPlan.Electron.StartOrder) != 4 {
		t.Fatalf("electron start order len = %d, want 4", len(hostPlan.Electron.StartOrder))
	}
	if hostPlan.Electron.StartOrder[0] != "state" || hostPlan.Electron.StartOrder[1] != "sandbox" {
		t.Fatalf("electron start order = %#v", hostPlan.Electron.StartOrder)
	}
	for _, name := range []string{"gateway.json", "worker.json", "state.json", "sandbox.json"} {
		data, err := os.ReadFile(filepath.Join(dir, "processes", name))
		if err != nil {
			t.Fatalf("process %s read error = %v", name, err)
		}
		if !strings.Contains(string(data), "\"binary\"") {
			t.Fatalf("process %s = %q", name, string(data))
		}
	}
	gatewayData, err := os.ReadFile(filepath.Join(dir, "processes", "gateway.json"))
	if err != nil {
		t.Fatalf("gateway process read error = %v", err)
	}
	if !strings.Contains(string(gatewayData), "\"depends_on\"") || !strings.Contains(string(gatewayData), "\"component\"") {
		t.Fatalf("gateway process = %q", string(gatewayData))
	}
}

func TestPrepareCommandWriteBundle(t *testing.T) {
	var stdout strings.Builder
	dir := t.TempDir()
	prepared, err := PrepareCommand(flagSet("runtime-stack-bundle"), langgraphcmd.BuildInfo{}, CommandOptions{
		Stdout: &stdout,
		Args:   []string{"-write-bundle=" + dir, "-preset=shared-remote", "-worker-addr=:19081"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil || prepared.RunFunc == nil {
		t.Fatal("PrepareCommand() did not build bundle run func")
	}
	if err := prepared.Run(); err != nil {
		t.Fatalf("prepared.Run() error = %v", err)
	}
	if strings.TrimSpace(stdout.String()) != dir {
		t.Fatalf("stdout = %q, want %q", stdout.String(), dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "stack-manifest.json")); err != nil {
		t.Fatalf("manifest stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "host-plan.json")); err != nil {
		t.Fatalf("host plan stat error = %v", err)
	}
}

func TestWriteBundleRejectsInvalidProcessGraph(t *testing.T) {
	dir := t.TempDir()
	manifest := StackManifest{
		Processes: []ProcessManifest{
			{Name: "gateway", DependsOn: []string{"worker"}},
		},
	}
	err := WriteBundle(dir, manifest)
	if err == nil || !strings.Contains(err.Error(), "invalid process graph") {
		t.Fatalf("WriteBundle() error = %v, want invalid process graph", err)
	}
}

func flagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}
