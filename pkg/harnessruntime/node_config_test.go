package harnessruntime

import "testing"

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
	config.State = RuntimeStateStoreConfig{
		Backend: RuntimeStateStoreBackendFile,
		Root:    t.TempDir(),
	}
	if _, ok := config.BuildRunSnapshotStore().(*JSONFileRunStore); !ok {
		t.Fatalf("BuildRunSnapshotStore() returned %T", config.BuildRunSnapshotStore())
	}
	if _, ok := config.BuildThreadStateStore().(*JSONFileThreadStateStore); !ok {
		t.Fatalf("BuildThreadStateStore() returned %T", config.BuildThreadStateStore())
	}
}
