package harnessruntime

import (
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

const defaultSandboxHeartbeatInterval = 30 * time.Second
const defaultSandboxIdleTTL = 2 * time.Minute
const defaultSandboxSweepInterval = 30 * time.Second

type SandboxLeaseConfig struct {
	HeartbeatInterval time.Duration
	IdleTTL           time.Duration
	SweepInterval     time.Duration
}

type SandboxLease struct {
	Sandbox           sandbox.Session
	Heartbeat         func() error
	Release           func() error
	HeartbeatInterval time.Duration
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

func (r leaseBackedSandboxRuntime) Resolve(req harness.AgentRequest) (sandbox.Session, error) {
	binding, err := r.Bind(req)
	if err != nil {
		return nil, err
	}
	return binding.Sandbox, nil
}

func (r leaseBackedSandboxRuntime) Bind(req harness.AgentRequest) (harness.SandboxBinding, error) {
	if r.leases == nil {
		return harness.SandboxBinding{}, nil
	}
	if r.policy != nil && !r.policy.Enabled(req) {
		return harness.SandboxBinding{}, nil
	}
	lease, err := r.leases.AcquireLease(req)
	if err != nil {
		return harness.SandboxBinding{}, err
	}
	return harness.SandboxBinding{
		Sandbox:           lease.Sandbox,
		Heartbeat:         lease.Heartbeat,
		Release:           lease.Release,
		HeartbeatInterval: lease.HeartbeatInterval,
	}, nil
}

func (r leaseBackedSandboxRuntime) Close() error {
	if r.leases == nil {
		return nil
	}
	return r.leases.Close()
}

type localSandboxLeaseService struct {
	provider          harness.SandboxProvider
	heartbeatInterval time.Duration
	idleTTL           time.Duration
	sweepInterval     time.Duration
	mu                sync.Mutex
	refs              int
	lastTouched       time.Time
	closed            bool
	stopCh            chan struct{}
	wg                sync.WaitGroup
}

func NewLocalSandboxLeaseService(name, root string) SandboxLeaseService {
	return NewLocalSandboxLeaseServiceWithConfig(name, root, SandboxLeaseConfig{})
}

func NewLocalSandboxLeaseServiceWithConfig(name, root string, config SandboxLeaseConfig) SandboxLeaseService {
	return NewLocalSandboxLeaseServiceWithSandboxConfig(name, root, config, sandbox.Config{})
}

func NewLocalSandboxLeaseServiceWithSandboxConfig(name, root string, config SandboxLeaseConfig, sandboxConfig sandbox.Config) SandboxLeaseService {
	normalized := normalizeSandboxLeaseConfig(config)
	service := &localSandboxLeaseService{
		provider:          newRuntimeLocalSandboxProvider(name, root, sandboxConfig),
		heartbeatInterval: normalized.HeartbeatInterval,
		idleTTL:           normalized.IdleTTL,
		sweepInterval:     normalized.SweepInterval,
		stopCh:            make(chan struct{}),
	}
	if normalized.IdleTTL > 0 && normalized.SweepInterval > 0 {
		service.wg.Add(1)
		go service.runJanitor()
	}
	return service
}

func normalizeSandboxLeaseConfig(config SandboxLeaseConfig) SandboxLeaseConfig {
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = defaultSandboxHeartbeatInterval
	}
	if config.IdleTTL <= 0 {
		config.IdleTTL = defaultSandboxIdleTTL
	}
	if config.SweepInterval <= 0 {
		config.SweepInterval = defaultSandboxSweepInterval
	}
	return config
}

func (s *localSandboxLeaseService) Provider() harness.SandboxProvider {
	return s.provider
}

func (s *localSandboxLeaseService) AcquireLease(req harness.AgentRequest) (SandboxLease, error) {
	if s.provider == nil {
		return SandboxLease{}, nil
	}
	sb, err := s.provider.Acquire()
	if err != nil {
		return SandboxLease{}, err
	}
	s.touch()
	s.mu.Lock()
	s.refs++
	s.mu.Unlock()
	return SandboxLease{
		Sandbox:           sb,
		HeartbeatInterval: s.heartbeatInterval,
		Heartbeat:         s.heartbeat,
		Release:           s.release,
	}, nil
}

func (s *localSandboxLeaseService) Close() error {
	if s == nil || s.provider == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.stopCh)
	s.mu.Unlock()
	s.wg.Wait()
	return s.provider.Close()
}

func (s *localSandboxLeaseService) heartbeat() error {
	s.touch()
	return nil
}

func (s *localSandboxLeaseService) touch() {
	s.mu.Lock()
	s.lastTouched = time.Now().UTC()
	s.mu.Unlock()
}

func (s *localSandboxLeaseService) release() error {
	if s.provider == nil {
		return nil
	}
	s.mu.Lock()
	if s.refs > 0 {
		s.refs--
	}
	if s.refs == 0 {
		s.lastTouched = time.Now().UTC()
	}
	s.mu.Unlock()
	return nil
}

func (s *localSandboxLeaseService) runJanitor() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.evictIdle()
		case <-s.stopCh:
			return
		}
	}
}

func (s *localSandboxLeaseService) evictIdle() {
	if s.provider == nil || s.idleTTL <= 0 {
		return
	}
	s.mu.Lock()
	if s.closed || s.refs > 0 {
		s.mu.Unlock()
		return
	}
	lastTouched := s.lastTouched
	s.mu.Unlock()
	if lastTouched.IsZero() || time.Since(lastTouched) < s.idleTTL {
		return
	}
	_ = s.provider.Close()
	s.touch()
}

type runtimeLocalSandboxProvider struct {
	name string
	root string
	cfg  sandbox.Config

	mu      sync.Mutex
	sandbox sandbox.Session
}

func newRuntimeLocalSandboxProvider(name, root string, cfg sandbox.Config) *runtimeLocalSandboxProvider {
	return &runtimeLocalSandboxProvider{name: name, root: root, cfg: cfg}
}

func (p *runtimeLocalSandboxProvider) Acquire() (sandbox.Session, error) {
	if p == nil {
		return nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sandbox != nil {
		return p.sandbox, nil
	}
	sb, err := sandbox.NewWithConfig(p.name, p.root, p.cfg)
	if err != nil {
		return nil, err
	}
	p.sandbox = sb
	return sb, nil
}

func (p *runtimeLocalSandboxProvider) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sandbox == nil {
		return nil
	}
	err := p.sandbox.Close()
	p.sandbox = nil
	return err
}
