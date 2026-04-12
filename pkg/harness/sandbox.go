package harness

import (
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

type SandboxBinding struct {
	Sandbox           *sandbox.Sandbox
	Heartbeat         func() error
	Release           func() error
	HeartbeatInterval time.Duration
}

// SandboxProvider mirrors upstream's provider boundary. The current local
// implementation keeps the existing singleton behavior, but runtime assembly no
// longer depends on compat-owned sandbox fields directly.
type SandboxProvider interface {
	Acquire() (*sandbox.Sandbox, error)
	Close() error
}

// SandboxPolicy decides whether a given agent request should acquire a sandbox.
// This keeps enablement decisions in runtime-owned code instead of factory
// callsites.
type SandboxPolicy interface {
	Enabled(AgentRequest) bool
}

type FeatureSandboxPolicy struct{}

func (FeatureSandboxPolicy) Enabled(req AgentRequest) bool {
	return req.Features.Sandbox
}

type AlwaysSandboxPolicy struct{}

func (AlwaysSandboxPolicy) Enabled(AgentRequest) bool {
	return true
}

// SandboxRuntime owns sandbox enablement and acquisition behind one runtime
// boundary so compat layers do not wire provider access directly.
type SandboxRuntime interface {
	Provider() SandboxProvider
	Resolve(AgentRequest) (*sandbox.Sandbox, error)
	Close() error
}

type ManagedSandboxRuntime interface {
	SandboxRuntime
	Bind(AgentRequest) (SandboxBinding, error)
}

type StaticSandboxRuntime struct {
	provider SandboxProvider
	policy   SandboxPolicy
}

func NewStaticSandboxRuntime(provider SandboxProvider, policy SandboxPolicy) *StaticSandboxRuntime {
	if policy == nil {
		policy = FeatureSandboxPolicy{}
	}
	return &StaticSandboxRuntime{
		provider: provider,
		policy:   policy,
	}
}

func (r *StaticSandboxRuntime) Provider() SandboxProvider {
	if r == nil {
		return nil
	}
	return r.provider
}

func (r *StaticSandboxRuntime) Resolve(req AgentRequest) (*sandbox.Sandbox, error) {
	if r == nil || r.provider == nil {
		return nil, nil
	}
	if r.policy != nil && !r.policy.Enabled(req) {
		return nil, nil
	}
	return r.provider.Acquire()
}

func (r *StaticSandboxRuntime) Close() error {
	if r == nil || r.provider == nil {
		return nil
	}
	return r.provider.Close()
}

func (r *StaticSandboxRuntime) Bind(req AgentRequest) (SandboxBinding, error) {
	sb, err := r.Resolve(req)
	if err != nil {
		return SandboxBinding{}, err
	}
	return SandboxBinding{Sandbox: sb}, nil
}

type LocalSandboxProvider struct {
	name string
	root string

	mu      sync.Mutex
	sandbox *sandbox.Sandbox
}

func NewLocalSandboxProvider(name, root string) *LocalSandboxProvider {
	return &LocalSandboxProvider{name: name, root: root}
}

func (p *LocalSandboxProvider) Acquire() (*sandbox.Sandbox, error) {
	if p == nil {
		return nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sandbox != nil {
		return p.sandbox, nil
	}
	sb, err := sandbox.New(p.name, p.root)
	if err != nil {
		return nil, err
	}
	p.sandbox = sb
	return sb, nil
}

func (p *LocalSandboxProvider) Close() error {
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
