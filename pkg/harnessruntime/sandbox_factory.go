package harnessruntime

import (
	"fmt"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

// SandboxManagerConfig describes the runtime-owned sandbox backend selection.
// It intentionally lives below compat so future distributed deployments can
// swap backend/lease implementations without changing HTTP wiring.
type SandboxManagerConfig struct {
	Backend SandboxBackend
	Name    string
	Root    string
	Policy  harness.SandboxPolicy
	Leases  SandboxLeaseService
}

func NewSandboxManagerFromConfig(config SandboxManagerConfig) (*SandboxResourceManager, error) {
	backend := config.Backend
	if backend == "" {
		backend = SandboxBackendLocalLinux
	}
	if config.Leases != nil {
		return NewSandboxResourceManager(backend, config.Leases), nil
	}
	switch backend {
	case SandboxBackendLocalLinux:
		return NewLocalSandboxManager(config.Name, config.Root), nil
	case SandboxBackendContainer, SandboxBackendRemote, SandboxBackendWindowsRestricted:
		return NewSandboxResourceManager(backend, unsupportedSandboxLeaseService{backend: backend}), nil
	default:
		return nil, fmt.Errorf("unsupported sandbox backend %q", backend)
	}
}

func NewSandboxRuntimeFromConfig(config SandboxManagerConfig) (harness.SandboxRuntime, error) {
	manager, err := NewSandboxManagerFromConfig(config)
	if err != nil {
		return nil, err
	}
	policy := config.Policy
	if policy == nil {
		policy = harness.FeatureSandboxPolicy{}
	}
	return manager.Runtime(policy), nil
}

type unsupportedSandboxLeaseService struct {
	backend SandboxBackend
}

func (s unsupportedSandboxLeaseService) Provider() harness.SandboxProvider {
	return nil
}

func (s unsupportedSandboxLeaseService) AcquireLease(harness.AgentRequest) (SandboxLease, error) {
	return SandboxLease{}, fmt.Errorf("sandbox backend %q is not configured", s.backend)
}

func (s unsupportedSandboxLeaseService) Close() error {
	return nil
}
