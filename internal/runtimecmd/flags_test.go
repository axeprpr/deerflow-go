package runtimecmd

import (
	"flag"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestBindFlagsBuildsNodeConfig(t *testing.T) {
	fs := flag.NewFlagSet("runtime", flag.ContinueOnError)
	defaults := DefaultRuntimeWorkerNodeConfig()
	binding := BindFlags(fs, defaults, "", "")
	if err := fs.Parse([]string{
		"-preset=shared-sqlite",
		"-role=all-in-one",
		"-addr=9091",
		"-transport-backend=direct",
		"-sandbox-backend=container",
		"-sandbox-image=debian:bookworm",
		"-sandbox-max-active-leases=12",
		"-memory-store=sqlite:///tmp/memory.sqlite3",
		"-state-store=sqlite:///tmp/runtime.sqlite3",
		"-snapshot-store=sqlite:///tmp/snapshots.sqlite3",
		"-event-store=sqlite:///tmp/events.sqlite3",
		"-thread-store=sqlite:///tmp/threads.sqlite3",
		"-state-backend=file",
		"-state-root=/tmp/state",
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	cfg := binding.Config()
	if cfg.Role != harnessruntime.RuntimeNodeRoleAllInOne {
		t.Fatalf("Role = %q", cfg.Role)
	}
	if cfg.Preset != RuntimeNodePresetSharedSQLite {
		t.Fatalf("Preset = %q", cfg.Preset)
	}
	if cfg.StateProvider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("StateProvider = %q", cfg.StateProvider)
	}
	if cfg.Addr != ":9091" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.TransportBackend != harnessruntime.WorkerTransportBackendDirect {
		t.Fatalf("TransportBackend = %q", cfg.TransportBackend)
	}
	if cfg.SandboxBackend != harnessruntime.SandboxBackendContainer {
		t.Fatalf("SandboxBackend = %q", cfg.SandboxBackend)
	}
	if cfg.SandboxImage != "debian:bookworm" {
		t.Fatalf("SandboxImage = %q", cfg.SandboxImage)
	}
	if cfg.SandboxMaxActiveLeases != 12 {
		t.Fatalf("SandboxMaxActiveLeases = %d", cfg.SandboxMaxActiveLeases)
	}
	if cfg.MemoryStoreURL != "sqlite:///tmp/memory.sqlite3" {
		t.Fatalf("MemoryStoreURL = %q", cfg.MemoryStoreURL)
	}
	if cfg.StateStoreURL != "sqlite:///tmp/runtime.sqlite3" {
		t.Fatalf("StateStoreURL = %q", cfg.StateStoreURL)
	}
	if cfg.SnapshotStoreURL != "sqlite:///tmp/snapshots.sqlite3" {
		t.Fatalf("SnapshotStoreURL = %q", cfg.SnapshotStoreURL)
	}
	if cfg.EventStoreURL != "sqlite:///tmp/events.sqlite3" {
		t.Fatalf("EventStoreURL = %q", cfg.EventStoreURL)
	}
	if cfg.ThreadStoreURL != "sqlite:///tmp/threads.sqlite3" {
		t.Fatalf("ThreadStoreURL = %q", cfg.ThreadStoreURL)
	}
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/state" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
}

func TestBindFlagsStateStoreURLOverridesStateBackend(t *testing.T) {
	fs := flag.NewFlagSet("runtime", flag.ContinueOnError)
	defaults := DefaultRuntimeWorkerNodeConfig()
	binding := BindFlags(fs, defaults, "", "")
	if err := fs.Parse([]string{
		"-state-backend=file",
		"-state-store=sqlite:///tmp/runtime.sqlite3",
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	cfg := binding.Config()
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
}

func TestBindFlagsSupportsWSL2SandboxBackend(t *testing.T) {
	fs := flag.NewFlagSet("runtime", flag.ContinueOnError)
	defaults := DefaultRuntimeWorkerNodeConfig()
	binding := BindFlags(fs, defaults, "", "")
	if err := fs.Parse([]string{"-sandbox-backend=wsl2"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	cfg := binding.Config()
	if cfg.SandboxBackend != harnessruntime.SandboxBackendWSL2 {
		t.Fatalf("SandboxBackend = %q", cfg.SandboxBackend)
	}
}

func TestBindFlagsDerivesBackendsFromStoreURLs(t *testing.T) {
	fs := flag.NewFlagSet("runtime", flag.ContinueOnError)
	defaults := DefaultRuntimeWorkerNodeConfig()
	binding := BindFlags(fs, defaults, "", "")
	if err := fs.Parse([]string{
		"-state-store=sqlite:///tmp/runtime.sqlite3",
		"-snapshot-store=file:///tmp/snapshots",
		"-event-store=sqlite:///tmp/events.sqlite3",
		"-thread-store=file:///tmp/threads",
		"-state-backend=in-memory",
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	cfg := binding.Config()
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.SnapshotBackend != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("SnapshotBackend = %q", cfg.SnapshotBackend)
	}
	if cfg.EventBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("EventBackend = %q", cfg.EventBackend)
	}
	if cfg.ThreadBackend != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("ThreadBackend = %q", cfg.ThreadBackend)
	}
}

func TestBindFlagsBuildsSharedRemoteGatewayConfig(t *testing.T) {
	fs := flag.NewFlagSet("runtime", flag.ContinueOnError)
	defaults := DefaultNodeConfigForRole(harnessruntime.RuntimeNodeRoleGateway)
	binding := BindFlags(fs, defaults, "", "")
	if err := fs.Parse([]string{
		"-preset=shared-remote",
		"-endpoint=http://worker:8081/dispatch",
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	cfg := binding.Config()
	if cfg.Preset != RuntimeNodePresetSharedRemote {
		t.Fatalf("Preset = %q", cfg.Preset)
	}
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendRemote {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateStoreURL != "http://worker:8081/state" {
		t.Fatalf("StateStoreURL = %q", cfg.StateStoreURL)
	}
}

func TestBindFlagsKeepsRemoteBackendsForSharedSQLitePresetOverride(t *testing.T) {
	fs := flag.NewFlagSet("runtime", flag.ContinueOnError)
	defaults := DefaultRuntimeWorkerNodeConfig()
	binding := BindFlags(fs, defaults, "", "")
	if err := fs.Parse([]string{
		"-preset=shared-sqlite",
		"-state-backend=remote",
		"-snapshot-backend=remote",
		"-event-backend=remote",
		"-thread-backend=remote",
		"-state-store=http://127.0.0.1:29082/state",
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	cfg := binding.Config()
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendRemote {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.SnapshotBackend != harnessruntime.RuntimeStateStoreBackendRemote {
		t.Fatalf("SnapshotBackend = %q", cfg.SnapshotBackend)
	}
	if cfg.EventBackend != harnessruntime.RuntimeStateStoreBackendRemote {
		t.Fatalf("EventBackend = %q", cfg.EventBackend)
	}
	if cfg.ThreadBackend != harnessruntime.RuntimeStateStoreBackendRemote {
		t.Fatalf("ThreadBackend = %q", cfg.ThreadBackend)
	}
	if cfg.SnapshotStoreURL != "" || cfg.EventStoreURL != "" || cfg.ThreadStoreURL != "" {
		t.Fatalf("unexpected per-store URLs: snapshot=%q event=%q thread=%q", cfg.SnapshotStoreURL, cfg.EventStoreURL, cfg.ThreadStoreURL)
	}
	if err := cfg.ValidateForRuntimeNode(); err != nil {
		t.Fatalf("ValidateForRuntimeNode() error = %v", err)
	}
}
