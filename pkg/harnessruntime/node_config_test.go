package harnessruntime

import "testing"

func TestDefaultRuntimeNodeConfigUsesQueuedLocalDefaults(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", "/tmp/runtime-test")
	if config.Sandbox.Backend != SandboxBackendLocalLinux {
		t.Fatalf("sandbox backend = %q, want %q", config.Sandbox.Backend, SandboxBackendLocalLinux)
	}
	if config.Dispatch.Topology != DispatchTopologyQueued {
		t.Fatalf("dispatch topology = %q, want %q", config.Dispatch.Topology, DispatchTopologyQueued)
	}
}
