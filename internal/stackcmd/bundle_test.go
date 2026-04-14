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
	if hostPlan.RuntimePolicy.RestartPolicy != string(bundleDefaultRestartPolicy) {
		t.Fatalf("runtime policy restart = %q", hostPlan.RuntimePolicy.RestartPolicy)
	}
	if hostPlan.RuntimePolicy.DependencyTimeoutMilli != bundleDefaultDependencyTimeout.Milliseconds() {
		t.Fatalf("runtime policy dependency timeout = %d", hostPlan.RuntimePolicy.DependencyTimeoutMilli)
	}
	if hostPlan.RuntimePolicy.FailureIsolation {
		t.Fatalf("runtime policy failure isolation = true, want false")
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
	for _, unit := range []string{
		"deerflow-runtime-gateway.service",
		"deerflow-runtime-worker.service",
		"deerflow-runtime-state.service",
		"deerflow-runtime-sandbox.service",
	} {
		data, err := os.ReadFile(filepath.Join(dir, "host", "systemd", unit))
		if err != nil {
			t.Fatalf("systemd unit %s read error = %v", unit, err)
		}
		text := string(data)
		if !strings.Contains(text, "[Unit]") || !strings.Contains(text, "ExecStart=") || !strings.Contains(text, "[Service]") {
			t.Fatalf("systemd unit %s = %q", unit, text)
		}
	}
	electronData, err := os.ReadFile(filepath.Join(dir, "host", "electron", "runtime-processes.json"))
	if err != nil {
		t.Fatalf("electron runtime processes read error = %v", err)
	}
	var electronBundle ElectronHostBundle
	if err := json.Unmarshal(electronData, &electronBundle); err != nil {
		t.Fatalf("electron runtime processes decode error = %v", err)
	}
	if len(electronBundle.Processes) != 4 {
		t.Fatalf("electron processes len = %d, want 4", len(electronBundle.Processes))
	}
	if electronBundle.Processes[0].Name != "state" {
		t.Fatalf("first electron process = %#v", electronBundle.Processes[0])
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

func TestPrepareCommandWriteBundleUsesBundlePolicyFlags(t *testing.T) {
	var stdout strings.Builder
	dir := t.TempDir()
	prepared, err := PrepareCommand(flagSet("runtime-stack-bundle-policy"), langgraphcmd.BuildInfo{}, CommandOptions{
		Stdout: &stdout,
		Args: []string{
			"-write-bundle=" + dir,
			"-preset=shared-remote",
			"-worker-addr=:19081",
			"-bundle-restart-policy=always",
			"-bundle-max-restarts=7",
			"-bundle-restart-delay=2s",
			"-bundle-dependency-timeout=90s",
			"-bundle-failure-isolation",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if err := prepared.Run(); err != nil {
		t.Fatalf("prepared.Run() error = %v", err)
	}
	hostPlanData, err := os.ReadFile(filepath.Join(dir, "host-plan.json"))
	if err != nil {
		t.Fatalf("host plan read error = %v", err)
	}
	var hostPlan HostPlan
	if err := json.Unmarshal(hostPlanData, &hostPlan); err != nil {
		t.Fatalf("host plan decode error = %v", err)
	}
	if hostPlan.RuntimePolicy.RestartPolicy != string(ProcessRestartAlways) {
		t.Fatalf("runtime policy restart=%q", hostPlan.RuntimePolicy.RestartPolicy)
	}
	if hostPlan.RuntimePolicy.MaxRestarts != 7 {
		t.Fatalf("runtime policy max restarts=%d", hostPlan.RuntimePolicy.MaxRestarts)
	}
	if hostPlan.RuntimePolicy.RestartDelayMilli != 2000 {
		t.Fatalf("runtime policy restart delay=%d", hostPlan.RuntimePolicy.RestartDelayMilli)
	}
	if hostPlan.RuntimePolicy.DependencyTimeoutMilli != 90000 {
		t.Fatalf("runtime policy dependency timeout=%d", hostPlan.RuntimePolicy.DependencyTimeoutMilli)
	}
	if !hostPlan.RuntimePolicy.FailureIsolation {
		t.Fatalf("runtime policy failure isolation = false")
	}
	if len(hostPlan.Processes) == 0 {
		t.Fatal("host plan processes = 0")
	}
	if hostPlan.Processes[0].RestartPolicy != string(ProcessRestartAlways) {
		t.Fatalf("process restart policy=%q", hostPlan.Processes[0].RestartPolicy)
	}
	if hostPlan.Processes[0].DependencyTimeoutMilli != 90000 {
		t.Fatalf("process dependency timeout=%d", hostPlan.Processes[0].DependencyTimeoutMilli)
	}
	if !hostPlan.Processes[0].FailureIsolation {
		t.Fatalf("process failure isolation=false")
	}
	gatewayUnit, err := os.ReadFile(filepath.Join(dir, "host", "systemd", "deerflow-runtime-gateway.service"))
	if err != nil {
		t.Fatalf("gateway systemd unit read error = %v", err)
	}
	if !strings.Contains(string(gatewayUnit), "Restart=always") || !strings.Contains(string(gatewayUnit), "RestartSec=2000ms") {
		t.Fatalf("gateway systemd unit = %q", string(gatewayUnit))
	}
	electronData, err := os.ReadFile(filepath.Join(dir, "host", "electron", "runtime-processes.json"))
	if err != nil {
		t.Fatalf("electron runtime processes read error = %v", err)
	}
	var electronBundle ElectronHostBundle
	if err := json.Unmarshal(electronData, &electronBundle); err != nil {
		t.Fatalf("electron runtime processes decode error = %v", err)
	}
	if len(electronBundle.Processes) == 0 || electronBundle.Processes[0].RestartPolicy != string(ProcessRestartAlways) {
		t.Fatalf("electron processes = %#v", electronBundle.Processes)
	}
}

func TestWriteBundleWithOptionsRejectsInvalidRestartPolicy(t *testing.T) {
	cfg := DefaultConfig()
	err := WriteBundleWithOptions(t.TempDir(), cfg.Manifest(), BundleOptions{
		RestartPolicy: ProcessRestartPolicy("sometimes"),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid restart policy") {
		t.Fatalf("WriteBundleWithOptions() error = %v", err)
	}
}

func TestPrepareCommandValidateBundle(t *testing.T) {
	var stdout strings.Builder
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"
	dir := t.TempDir()
	if err := WriteBundle(dir, cfg.Manifest()); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}

	prepared, err := PrepareCommand(flagSet("runtime-stack-validate-bundle"), langgraphcmd.BuildInfo{}, CommandOptions{
		Stdout: &stdout,
		Args:   []string{"-validate-bundle=" + dir},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil || prepared.RunFunc == nil {
		t.Fatal("PrepareCommand() did not build validate run func")
	}
	if err := prepared.Run(); err != nil {
		t.Fatalf("prepared.Run() error = %v", err)
	}
	if strings.TrimSpace(stdout.String()) != dir {
		t.Fatalf("stdout = %q, want %q", stdout.String(), dir)
	}
}

func TestValidateBundleRejectsMissingHostAsset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"
	dir := t.TempDir()
	if err := WriteBundle(dir, cfg.Manifest()); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "host", "electron", "runtime-processes.json")); err != nil {
		t.Fatalf("remove electron bundle error = %v", err)
	}

	err := ValidateBundle(dir)
	if err == nil || !strings.Contains(err.Error(), "host/electron/runtime-processes.json") {
		t.Fatalf("ValidateBundle() error = %v", err)
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
