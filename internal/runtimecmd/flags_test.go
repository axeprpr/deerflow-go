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
		"-role=all-in-one",
		"-addr=9091",
		"-transport-backend=direct",
		"-sandbox-backend=container",
		"-sandbox-image=debian:bookworm",
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
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/state" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
}
