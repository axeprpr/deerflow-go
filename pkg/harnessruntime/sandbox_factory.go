package harnessruntime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

// SandboxManagerConfig describes the runtime-owned sandbox backend selection.
// It intentionally lives below compat so future distributed deployments can
// swap backend/lease implementations without changing HTTP wiring.
type SandboxManagerConfig struct {
	Backend           SandboxBackend
	Name              string
	Root              string
	Endpoint          string
	Image             string
	MaxActiveLeases   int
	Policy            harness.SandboxPolicy
	HeartbeatInterval time.Duration
	IdleTTL           time.Duration
	SweepInterval     time.Duration
	Leases            SandboxLeaseService
}

func (c SandboxManagerConfig) Normalized() SandboxManagerConfig {
	if c.Backend == "" {
		c.Backend = SandboxBackendLocalLinux
	}
	c.Name = strings.TrimSpace(c.Name)
	c.Root = strings.TrimSpace(c.Root)
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.Image = strings.TrimSpace(c.Image)
	if c.MaxActiveLeases < 0 {
		c.MaxActiveLeases = 0
	}
	return c
}

func (c SandboxManagerConfig) Validate() error {
	config := c.Normalized()
	switch config.Backend {
	case SandboxBackendLocalLinux:
		return nil
	case SandboxBackendContainer:
		if config.Image == "" {
			return fmt.Errorf("container sandbox backend requires image")
		}
		return nil
	case SandboxBackendRemote:
		if config.Endpoint == "" {
			return fmt.Errorf("remote sandbox backend requires endpoint")
		}
		return nil
	case SandboxBackendWindowsRestricted:
		return nil
	default:
		return fmt.Errorf("unsupported sandbox backend %q", config.Backend)
	}
}

type SandboxLeaseServiceFactory interface {
	Build(SandboxManagerConfig) (SandboxLeaseService, error)
}

type SandboxLeaseServiceFactoryFunc func(SandboxManagerConfig) (SandboxLeaseService, error)

func (f SandboxLeaseServiceFactoryFunc) Build(config SandboxManagerConfig) (SandboxLeaseService, error) {
	return f(config)
}

type SandboxManagerFactory struct {
	factories map[SandboxBackend]SandboxLeaseServiceFactory
}

func DefaultSandboxManagerFactory() SandboxManagerFactory {
	return SandboxManagerFactory{
		factories: map[SandboxBackend]SandboxLeaseServiceFactory{
			SandboxBackendLocalLinux: SandboxLeaseServiceFactoryFunc(func(config SandboxManagerConfig) (SandboxLeaseService, error) {
				return NewLocalSandboxLeaseServiceWithConfig(config.Name, config.Root, SandboxLeaseConfig{
					HeartbeatInterval: config.HeartbeatInterval,
					IdleTTL:           config.IdleTTL,
					SweepInterval:     config.SweepInterval,
				}), nil
			}),
			SandboxBackendContainer: SandboxLeaseServiceFactoryFunc(func(config SandboxManagerConfig) (SandboxLeaseService, error) {
				name, root, lease := localSandboxLeaseDefaults(config, SandboxBackendContainer)
				return NewLocalSandboxLeaseServiceWithConfig(name, root, lease), nil
			}),
			SandboxBackendRemote: SandboxLeaseServiceFactoryFunc(func(config SandboxManagerConfig) (SandboxLeaseService, error) {
				return NewRemoteSandboxLeaseService(config.Endpoint, nil), nil
			}),
			SandboxBackendWindowsRestricted: SandboxLeaseServiceFactoryFunc(func(config SandboxManagerConfig) (SandboxLeaseService, error) {
				name, root, lease := localSandboxLeaseDefaults(config, SandboxBackendWindowsRestricted)
				return NewLocalSandboxLeaseServiceWithConfig(name, root, lease), nil
			}),
		},
	}
}

func (f SandboxManagerFactory) Build(config SandboxManagerConfig) (*SandboxResourceManager, error) {
	config = config.Normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	backend := config.Backend
	if config.Leases != nil {
		return NewSandboxResourceManager(backend, config.Leases), nil
	}
	factory, ok := f.factories[backend]
	if !ok {
		return nil, fmt.Errorf("unsupported sandbox backend %q", backend)
	}
	leases, err := factory.Build(config)
	if err != nil {
		return nil, err
	}
	return NewSandboxResourceManager(backend, leases), nil
}

func NewSandboxManagerFromConfig(config SandboxManagerConfig) (*SandboxResourceManager, error) {
	return DefaultSandboxManagerFactory().Build(config)
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

func localSandboxLeaseDefaults(config SandboxManagerConfig, backend SandboxBackend) (name string, root string, lease SandboxLeaseConfig) {
	name = strings.TrimSpace(config.Name)
	if name == "" {
		name = "runtime-" + string(backend)
	}
	root = strings.TrimSpace(config.Root)
	if root == "" {
		root = filepath.Join(os.TempDir(), "deerflow-sandbox", string(backend))
	}
	lease = SandboxLeaseConfig{
		HeartbeatInterval: config.HeartbeatInterval,
		IdleTTL:           config.IdleTTL,
		SweepInterval:     config.SweepInterval,
	}
	return name, root, lease
}

type unsupportedSandboxLeaseService struct {
	backend SandboxBackend
	detail  string
}

func newUnsupportedSandboxLeaseService(config SandboxManagerConfig, detail string) unsupportedSandboxLeaseService {
	return unsupportedSandboxLeaseService{
		backend: config.Backend,
		detail:  detail,
	}
}

func (s unsupportedSandboxLeaseService) Provider() harness.SandboxProvider {
	return nil
}

func (s unsupportedSandboxLeaseService) AcquireLease(harness.AgentRequest) (SandboxLease, error) {
	if s.detail != "" {
		return SandboxLease{}, fmt.Errorf("%s", s.detail)
	}
	return SandboxLease{}, fmt.Errorf("sandbox backend %q is not configured", s.backend)
}

func (s unsupportedSandboxLeaseService) Close() error {
	return nil
}
