package harnessruntime

import (
	"path/filepath"
	"testing"
)

func TestDefaultRuntimeNodeConfigUsesQueuedLocalDefaults(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", "/tmp/runtime-test")
	if config.Sandbox.Backend != SandboxBackendLocalLinux {
		t.Fatalf("sandbox backend = %q, want %q", config.Sandbox.Backend, SandboxBackendLocalLinux)
	}
	if config.Transport.Backend != WorkerTransportBackendQueue {
		t.Fatalf("transport backend = %q, want %q", config.Transport.Backend, WorkerTransportBackendQueue)
	}
	if config.State.Backend != RuntimeStateStoreBackendInMemory {
		t.Fatalf("state backend = %q, want %q", config.State.Backend, RuntimeStateStoreBackendInMemory)
	}
}

func TestRuntimeNodeConfigBuildsFileBackedStateStores(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	root := t.TempDir()
	config.State = RuntimeStateStoreConfig{
		Backend: RuntimeStateStoreBackendFile,
		Root:    root,
	}
	runStore, ok := config.BuildRunSnapshotStore().(*JSONFileRunStore)
	if !ok {
		t.Fatalf("BuildRunSnapshotStore() returned %T", config.BuildRunSnapshotStore())
	}
	threadStore, ok := config.BuildThreadStateStore().(*JSONFileThreadStateStore)
	if !ok {
		t.Fatalf("BuildThreadStateStore() returned %T", config.BuildThreadStateStore())
	}
	if runStore.root != filepath.Join(root, "runs") {
		t.Fatalf("runStore.root = %q", runStore.root)
	}
	if threadStore.root != filepath.Join(root, "threads") {
		t.Fatalf("threadStore.root = %q", threadStore.root)
	}
}
