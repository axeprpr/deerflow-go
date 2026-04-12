package harnessruntime

import (
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

type SandboxLease struct {
	Sandbox   *sandbox.Sandbox
	Heartbeat func() error
	Release   func() error
}

type SandboxLeaseService interface {
	Provider() harness.SandboxProvider
	AcquireLease(harness.AgentRequest) (SandboxLease, error)
	Close() error
}

type leaseBackedSandboxRuntime struct {
	leases SandboxLeaseService
	policy harness.SandboxPolicy
}

func NewLeaseBackedSandboxRuntime(leases SandboxLeaseService, policy harness.SandboxPolicy) harness.SandboxRuntime {
	if policy == nil {
		policy = harness.FeatureSandboxPolicy{}
	}
	return leaseBackedSandboxRuntime{
		leases: leases,
		policy: policy,
	}
}

func (r leaseBackedSandboxRuntime) Provider() harness.SandboxProvider {
	if r.leases == nil {
		return nil
	}
	return r.leases.Provider()
}

func (r leaseBackedSandboxRuntime) Resolve(req harness.AgentRequest) (*sandbox.Sandbox, error) {
	if r.leases == nil {
		return nil, nil
	}
	if r.policy != nil && !r.policy.Enabled(req) {
		return nil, nil
	}
	lease, err := r.leases.AcquireLease(req)
	if err != nil {
		return nil, err
	}
	return lease.Sandbox, nil
}

func (r leaseBackedSandboxRuntime) Close() error {
	if r.leases == nil {
		return nil
	}
	return r.leases.Close()
}

type localSandboxLeaseService struct {
	provider harness.SandboxProvider
}

func NewLocalSandboxLeaseService(name, root string) SandboxLeaseService {
	return localSandboxLeaseService{
		provider: harness.NewLocalSandboxProvider(name, root),
	}
}

func (s localSandboxLeaseService) Provider() harness.SandboxProvider {
	return s.provider
}

func (s localSandboxLeaseService) AcquireLease(req harness.AgentRequest) (SandboxLease, error) {
	if s.provider == nil {
		return SandboxLease{}, nil
	}
	sb, err := s.provider.Acquire()
	if err != nil {
		return SandboxLease{}, err
	}
	return SandboxLease{
		Sandbox: sb,
		Heartbeat: func() error {
			return nil
		},
		Release: func() error {
			return nil
		},
	}, nil
}

func (s localSandboxLeaseService) Close() error {
	if s.provider == nil {
		return nil
	}
	return s.provider.Close()
}

