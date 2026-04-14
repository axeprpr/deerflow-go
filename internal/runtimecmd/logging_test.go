package runtimecmd

import (
	"strings"
	"testing"
)

func TestNodeConfigStartupLines(t *testing.T) {
	cfg := DefaultRuntimeWorkerNodeConfig()
	cfg.MemoryStoreURL = "sqlite:///tmp/memory.sqlite3"
	cfg.StateStoreURL = "sqlite:///tmp/runtime.sqlite3"
	cfg.SnapshotStoreURL = "sqlite:///tmp/snapshots.sqlite3"
	cfg.EventStoreURL = "sqlite:///tmp/events.sqlite3"
	cfg.ThreadStoreURL = "sqlite:///tmp/threads.sqlite3"
	lines := cfg.StartupLines()
	if len(lines) == 0 {
		t.Fatal("StartupLines() returned no lines")
	}
	joined := strings.Join(lines, "\n")
	manifest := cfg.Manifest()
	if !strings.Contains(joined, "runtime node starting role=worker") {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "preset="+string(manifest.Preset)) {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "state_provider="+string(manifest.StateProvider)) {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "worker_addr=") {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "memory_store=sqlite:///tmp/memory.sqlite3") {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "store=sqlite:///tmp/runtime.sqlite3") {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "snapshot_store=sqlite:///tmp/snapshots.sqlite3") {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "root=") {
		t.Fatalf("StartupLines() = %q", joined)
	}
}
