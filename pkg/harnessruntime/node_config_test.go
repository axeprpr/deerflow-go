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
	if config.State.SnapshotBackend != RuntimeStateStoreBackendInMemory || config.State.EventBackend != RuntimeStateStoreBackendInMemory || config.State.ThreadBackend != RuntimeStateStoreBackendInMemory {
		t.Fatalf("state config = %+v", config.State)
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
	if eventStore, ok := config.BuildRunEventStore().(*JSONFileRunEventStore); !ok {
		t.Fatalf("BuildRunEventStore() returned %T", config.BuildRunEventStore())
	} else if eventStore.root != filepath.Join(root, "events") {
		t.Fatalf("eventStore.root = %q", eventStore.root)
	}
}

func TestRuntimeNodeConfigSupportsMixedStateBackends(t *testing.T) {
	root := t.TempDir()
	config := DefaultRuntimeNodeConfig("runtime-test", root)
	config.State = RuntimeStateStoreConfig{
		SnapshotBackend: RuntimeStateStoreBackendFile,
		EventBackend:    RuntimeStateStoreBackendInMemory,
		ThreadBackend:   RuntimeStateStoreBackendInMemory,
		Root:            root,
	}
	if _, ok := config.BuildRunSnapshotStore().(*JSONFileRunStore); !ok {
		t.Fatalf("BuildRunSnapshotStore() returned %T", config.BuildRunSnapshotStore())
	}
	if _, ok := config.BuildThreadStateStore().(*InMemoryThreadStateStore); !ok {
		t.Fatalf("BuildThreadStateStore() returned %T", config.BuildThreadStateStore())
	}
	if _, ok := config.BuildRunEventStore().(*InMemoryRunEventStore); !ok {
		t.Fatalf("BuildRunEventStore() returned %T", config.BuildRunEventStore())
	}
}
