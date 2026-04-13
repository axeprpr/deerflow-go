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
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/state" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
}
