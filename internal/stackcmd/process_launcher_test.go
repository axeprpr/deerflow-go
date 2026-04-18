package stackcmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNewProcessLauncherRejectsMissingBinary(t *testing.T) {
	_, err := NewProcessLauncher([]ProcessManifest{
		{
			Name:   "gateway",
			Binary: "definitely-missing-runtime-binary",
		},
	}, ProcessLaunchOptions{})
	if err == nil {
		t.Fatal("NewProcessLauncher() error = nil, want missing executable")
	}
}

func TestResolveProcessOrderHonorsDependencies(t *testing.T) {
	order, err := resolveProcessOrder([]ProcessManifest{
		{Name: "gateway", DependsOn: []string{"worker"}},
		{Name: "worker", DependsOn: []string{"state", "sandbox"}},
		{Name: "state"},
		{Name: "sandbox"},
	})
	if err != nil {
		t.Fatalf("resolveProcessOrder() error = %v", err)
	}
	if !reflect.DeepEqual(order, []string{"state", "sandbox", "worker", "gateway"}) {
		t.Fatalf("order=%v", order)
	}
}

func TestNewProcessLauncherBuildsDependenciesAndPolicyDefaults(t *testing.T) {
	binDir := installTestProcessBinaries(t, "gateway-bin", "worker-bin", "state-bin")
	launcher, err := NewProcessLauncher([]ProcessManifest{
		{Name: "gateway", Binary: "gateway-bin", ReadyURL: "http://127.0.0.1:19080/healthz", DependsOn: []string{"worker"}},
		{Name: "worker", Binary: "worker-bin", ReadyURL: "http://127.0.0.1:19081/healthz", DependsOn: []string{"state"}},
		{Name: "state", Binary: "state-bin", ReadyURL: "http://127.0.0.1:19082/healthz"},
	}, ProcessLaunchOptions{
		BinaryDir:         binDir,
		DependencyTimeout: 5 * time.Second,
		RestartDelay:      200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewProcessLauncher() error = %v", err)
	}
	if len(launcher.lifecycles) != 3 {
		t.Fatalf("lifecycles=%d", len(launcher.lifecycles))
	}
	if launcher.lifecycles[2].name != "gateway" {
		t.Fatalf("last lifecycle=%q want gateway", launcher.lifecycles[2].name)
	}
	if !reflect.DeepEqual(launcher.lifecycles[2].dependencies, []string{"http://127.0.0.1:19081/healthz"}) {
		t.Fatalf("gateway deps=%v", launcher.lifecycles[2].dependencies)
	}
	if launcher.lifecycles[2].restartPolicy != ProcessRestartOnFailure {
		t.Fatalf("restart policy=%q", launcher.lifecycles[2].restartPolicy)
	}
	if launcher.failureIsolation {
		t.Fatal("failure isolation = true, want false")
	}
}

func TestNewProcessLauncherAppliesPerProcessPolicyOverrides(t *testing.T) {
	binDir := installTestProcessBinaries(t, "gateway-bin", "worker-bin")
	launcher, err := NewProcessLauncher([]ProcessManifest{
		{Name: "gateway", Binary: "gateway-bin", ReadyURL: "http://127.0.0.1:29080/healthz", DependsOn: []string{"worker"}},
		{Name: "worker", Binary: "worker-bin", ReadyURL: "http://127.0.0.1:29081/healthz"},
	}, ProcessLaunchOptions{
		BinaryDir:         binDir,
		RestartPolicy:     ProcessRestartOnFailure,
		MaxRestarts:       7,
		RestartDelay:      900 * time.Millisecond,
		DependencyTimeout: 35 * time.Second,
		ProcessPolicies: map[string]ProcessPolicy{
			"worker": {
				RestartPolicy:     ProcessRestartNever,
				MaxRestarts:       1,
				RestartDelay:      250 * time.Millisecond,
				DependencyTimeout: 12 * time.Second,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewProcessLauncher() error = %v", err)
	}
	if len(launcher.lifecycles) != 2 {
		t.Fatalf("lifecycles=%d want 2", len(launcher.lifecycles))
	}
	if launcher.lifecycles[0].name != "worker" {
		t.Fatalf("first lifecycle=%q want worker", launcher.lifecycles[0].name)
	}
	if launcher.lifecycles[0].restartPolicy != ProcessRestartNever {
		t.Fatalf("worker restart policy=%q want=%q", launcher.lifecycles[0].restartPolicy, ProcessRestartNever)
	}
	if launcher.lifecycles[0].maxRestarts != 1 {
		t.Fatalf("worker max restarts=%d want=1", launcher.lifecycles[0].maxRestarts)
	}
	if launcher.lifecycles[0].restartDelay != 250*time.Millisecond {
		t.Fatalf("worker restart delay=%s want=250ms", launcher.lifecycles[0].restartDelay)
	}
	if launcher.lifecycles[0].dependencyTimeout != 12*time.Second {
		t.Fatalf("worker dependency timeout=%s want=12s", launcher.lifecycles[0].dependencyTimeout)
	}
	if launcher.lifecycles[1].restartPolicy != ProcessRestartOnFailure {
		t.Fatalf("gateway restart policy=%q want=%q", launcher.lifecycles[1].restartPolicy, ProcessRestartOnFailure)
	}
	if launcher.lifecycles[1].maxRestarts != 7 {
		t.Fatalf("gateway max restarts=%d want=7", launcher.lifecycles[1].maxRestarts)
	}
	if launcher.lifecycles[1].restartDelay != 900*time.Millisecond {
		t.Fatalf("gateway restart delay=%s want=900ms", launcher.lifecycles[1].restartDelay)
	}
	if launcher.lifecycles[1].dependencyTimeout != 35*time.Second {
		t.Fatalf("gateway dependency timeout=%s want=35s", launcher.lifecycles[1].dependencyTimeout)
	}
}

func TestParseProcessRestartPolicy(t *testing.T) {
	tests := []struct {
		in   string
		want ProcessRestartPolicy
		err  bool
	}{
		{in: "", want: ProcessRestartOnFailure},
		{in: string(ProcessRestartNever), want: ProcessRestartNever},
		{in: string(ProcessRestartOnFailure), want: ProcessRestartOnFailure},
		{in: string(ProcessRestartAlways), want: ProcessRestartAlways},
		{in: "ALWAYS", want: ProcessRestartAlways},
		{in: "sometimes", err: true},
	}
	for _, tc := range tests {
		got, err := parseProcessRestartPolicy(tc.in)
		if tc.err {
			if err == nil {
				t.Fatalf("parseProcessRestartPolicy(%q) error = nil, want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseProcessRestartPolicy(%q) error = %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseProcessRestartPolicy(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func installTestProcessBinaries(t *testing.T, names ...string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	dir := t.TempDir()
	for _, name := range names {
		dst := filepath.Join(dir, name)
		data, err := os.ReadFile(exe)
		if err != nil {
			t.Fatalf("read executable %s: %v", exe, err)
		}
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			t.Fatalf("write executable %s: %v", dst, err)
		}
	}
	return dir
}
