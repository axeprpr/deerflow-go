package harnessruntime

import "github.com/axeprpr/deerflow-go/pkg/harness"

type SandboxBackend string

const (
	SandboxBackendLocalLinux        SandboxBackend = "local-linux"
	SandboxBackendContainer         SandboxBackend = "container"
	SandboxBackendRemote            SandboxBackend = "remote"
	SandboxBackendWindowsRestricted SandboxBackend = "windows-restricted"
)

type SandboxResourceManager struct {
	backend SandboxBackend
	leases  SandboxLeaseService
}

func NewSandboxResourceManager(backend SandboxBackend, leases SandboxLeaseService) *SandboxResourceManager {
	return &SandboxResourceManager{
		backend: backend,
		leases:  leases,
	}
}

func NewLocalSandboxManager(name, root string) *SandboxResourceManager {
	return NewLocalSandboxManagerWithConfig(name, root, SandboxLeaseConfig{})
}

func NewLocalSandboxManagerWithConfig(name, root string, config SandboxLeaseConfig) *SandboxResourceManager {
	return NewSandboxResourceManager(SandboxBackendLocalLinux, NewLocalSandboxLeaseServiceWithConfig(name, root, config))
}

func NewContainerSandboxManager(config SandboxManagerConfig) (*SandboxResourceManager, error) {
	config.Backend = SandboxBackendContainer
	return NewSandboxManagerFromConfig(config)
}

func NewRemoteSandboxManager(config SandboxManagerConfig) (*SandboxResourceManager, error) {
	config.Backend = SandboxBackendRemote
	return NewSandboxManagerFromConfig(config)
}

func NewWindowsRestrictedSandboxManager(config SandboxManagerConfig) (*SandboxResourceManager, error) {
	config.Backend = SandboxBackendWindowsRestricted
	return NewSandboxManagerFromConfig(config)
}

func (m *SandboxResourceManager) Backend() SandboxBackend {
	if m == nil {
		return ""
	}
	return m.backend
}

func (m *SandboxResourceManager) Runtime(policy harness.SandboxPolicy) harness.SandboxRuntime {
	if m == nil {
		return nil
	}
	return NewLeaseBackedSandboxRuntime(m.leases, policy)
}

func (m *SandboxResourceManager) Close() error {
	if m == nil || m.leases == nil {
		return nil
	}
	return m.leases.Close()
}
