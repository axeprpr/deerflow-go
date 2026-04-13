package harnessruntime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if lease.HeartbeatInterval <= 0 {
		t.Fatalf("heartbeat interval = %s, want > 0", lease.HeartbeatInterval)
	}
}

func TestLocalSandboxLeaseServiceReleasesProviderOnLastLease(t *testing.T) {
	service := &localSandboxLeaseService{
		provider:          &fakeSandboxProvider{},
		heartbeatInterval: defaultSandboxHeartbeatInterval,
		idleTTL:           defaultSandboxIdleTTL,
		sweepInterval:     defaultSandboxSweepInterval,
		stopCh:            make(chan struct{}),
	}

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
	if provider.closes != 0 {
		t.Fatalf("closes = %d, want 0 before idle eviction", provider.closes)
	}
}

func TestLocalSandboxLeaseServiceEvictsIdleProvider(t *testing.T) {
	service := NewLocalSandboxLeaseServiceWithConfig("runtime-lease", t.TempDir(), SandboxLeaseConfig{
		HeartbeatInterval: 5 * time.Millisecond,
		IdleTTL:           10 * time.Millisecond,
		SweepInterval:     5 * time.Millisecond,
	}).(*localSandboxLeaseService)

	provider := &fakeSandboxProvider{}
	service.provider = provider

	lease, err := service.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if provider.closes > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("provider closes = %d, want > 0 after idle eviction", provider.closes)
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

func TestSandboxManagerFromConfigSupportsLocalBackend(t *testing.T) {
	manager, err := NewSandboxManagerFromConfig(SandboxManagerConfig{
		Backend: SandboxBackendLocalLinux,
		Name:    "runtime-test",
		Root:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewSandboxManagerFromConfig() error = %v", err)
	}
	if manager == nil || manager.Backend() != SandboxBackendLocalLinux {
		t.Fatalf("manager = %#v", manager)
	}
}

func TestSandboxManagerFromConfigReturnsDeterministicUnsupportedBackendError(t *testing.T) {
	manager, err := NewSandboxManagerFromConfig(SandboxManagerConfig{
		Backend:  SandboxBackendRemote,
		Endpoint: "https://sandbox.internal",
	})
	if err != nil {
		t.Fatalf("NewSandboxManagerFromConfig() error = %v", err)
	}
	runtime := manager.Runtime(harness.FeatureSandboxPolicy{})
	_, err = runtime.Resolve(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err == nil {
		t.Fatal("Resolve() error = nil, want unsupported backend error")
	}
	want := "remote sandbox backend is not configured"
	if err.Error() != want {
		t.Fatalf("Resolve() error = %q, want %q", err.Error(), want)
	}
}

func TestBackendSpecificSandboxManagersExposeConfiguredBackend(t *testing.T) {
	containerManager, err := NewContainerSandboxManager(SandboxManagerConfig{
		Image: "ghcr.io/example/sandbox:latest",
	})
	if err != nil {
		t.Fatalf("NewContainerSandboxManager() error = %v", err)
	}
	if containerManager.Backend() != SandboxBackendContainer {
		t.Fatalf("container backend = %q", containerManager.Backend())
	}
	remoteManager, err := NewRemoteSandboxManager(SandboxManagerConfig{
		Endpoint: "https://sandbox.internal",
	})
	if err != nil {
		t.Fatalf("NewRemoteSandboxManager() error = %v", err)
	}
	if remoteManager.Backend() != SandboxBackendRemote {
		t.Fatalf("remote backend = %q", remoteManager.Backend())
	}
	windowsManager, err := NewWindowsRestrictedSandboxManager(SandboxManagerConfig{})
	if err != nil {
		t.Fatalf("NewWindowsRestrictedSandboxManager() error = %v", err)
	}
	if windowsManager.Backend() != SandboxBackendWindowsRestricted {
		t.Fatalf("windows backend = %q", windowsManager.Backend())
	}
}

func TestSandboxManagerFromConfigValidatesBackendRequirements(t *testing.T) {
	if _, err := NewSandboxManagerFromConfig(SandboxManagerConfig{
		Backend: SandboxBackendRemote,
	}); err == nil || err.Error() != "remote sandbox backend requires endpoint" {
		t.Fatalf("remote error = %v", err)
	}
	if _, err := NewSandboxManagerFromConfig(SandboxManagerConfig{
		Backend: SandboxBackendContainer,
	}); err == nil || err.Error() != "container sandbox backend requires image" {
		t.Fatalf("container error = %v", err)
	}
}

type fakeSandboxProvider struct {
	acquires int
	closes   int
}

func (p *fakeSandboxProvider) Acquire() (sandbox.Session, error) {
	p.acquires++
	return &sandbox.Sandbox{}, nil
}

func (p *fakeSandboxProvider) Close() error {
	p.closes++
	return nil
}
