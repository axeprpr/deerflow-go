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
}
