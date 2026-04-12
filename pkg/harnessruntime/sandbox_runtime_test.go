package harnessruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

func TestNewLocalSandboxRuntimeDefersSandboxCreationUntilEnabled(t *testing.T) {
	tmp := t.TempDir()
	runtime := NewLocalSandboxRuntime("runtime-test", tmp)

	sandboxDir := filepath.Join(tmp, "runtime-test")
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Fatalf("sandbox dir exists before resolve: err=%v", err)
	}

	sb, err := runtime.Resolve(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: false}})
	if err != nil {
		t.Fatalf("Resolve(disabled) error = %v", err)
	}
	if sb != nil {
		t.Fatalf("Resolve(disabled) returned sandbox = %#v, want nil", sb)
	}
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Fatalf("sandbox dir exists after disabled resolve: err=%v", err)
	}

	sb, err = runtime.Resolve(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("Resolve(enabled) error = %v", err)
	}
	if sb == nil {
		t.Fatal("Resolve(enabled) returned nil sandbox")
	}
	if _, err := os.Stat(sandboxDir); err != nil {
		t.Fatalf("sandbox dir missing after enabled resolve: %v", err)
	}
}

func TestNewLocalSandboxLeaseServiceAcquiresLease(t *testing.T) {
	tmp := t.TempDir()
	leases := NewLocalSandboxLeaseService("runtime-lease", tmp)

	lease, err := leases.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	if lease.Sandbox == nil {
		t.Fatal("AcquireLease() returned nil sandbox")
	}
	if lease.Heartbeat == nil || lease.Release == nil {
		t.Fatalf("lease callbacks = %#v", lease)
	}
}

func TestLocalSandboxLeaseServiceReleasesProviderOnLastLease(t *testing.T) {
	service := &localSandboxLeaseService{provider: &fakeSandboxProvider{}}

	lease1, err := service.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	lease2, err := service.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}

	provider := service.provider.(*fakeSandboxProvider)
	if provider.closes != 0 {
		t.Fatalf("closes = %d, want 0 before release", provider.closes)
	}
	if err := lease1.Release(); err != nil {
		t.Fatalf("lease1.Release() error = %v", err)
	}
	if provider.closes != 0 {
		t.Fatalf("closes = %d, want 0 after first release", provider.closes)
	}
	if err := lease2.Release(); err != nil {
		t.Fatalf("lease2.Release() error = %v", err)
	}
	if provider.closes != 1 {
		t.Fatalf("closes = %d, want 1 after last release", provider.closes)
	}
}

func TestLocalSandboxManagerExposesBackendAndRuntime(t *testing.T) {
	manager := NewLocalSandboxManager("runtime-test", t.TempDir())
	if manager.Backend() != SandboxBackendLocalLinux {
		t.Fatalf("backend = %q, want %q", manager.Backend(), SandboxBackendLocalLinux)
	}
	if manager.Runtime(harness.FeatureSandboxPolicy{}) == nil {
		t.Fatal("Runtime() = nil")
	}
}

type fakeSandboxProvider struct {
	acquires int
	closes   int
}

func (p *fakeSandboxProvider) Acquire() (*sandbox.Sandbox, error) {
	p.acquires++
	return &sandbox.Sandbox{}, nil
}

func (p *fakeSandboxProvider) Close() error {
	p.closes++
	return nil
}
