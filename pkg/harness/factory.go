package harness

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
)

type PreparedAgent struct {
	Agent             *agent.Agent
	Heartbeat         func() error
	Release           func() error
	HeartbeatInterval time.Duration
}

// Factory owns runtime assembly. Compat and HTTP layers should depend on this
// boundary instead of constructing agents directly.
type Factory struct {
	deps RuntimeDeps
}

func NewFactory(deps RuntimeDeps) *Factory {
	return &Factory{deps: deps}
}

func (f *Factory) NewAgent(req AgentRequest) (*agent.Agent, error) {
	prepared, err := f.PrepareAgent(req)
	if err != nil {
		return nil, err
	}
	return prepared.Agent, nil
}

func (f *Factory) PrepareAgent(req AgentRequest) (*PreparedAgent, error) {
	cfg := req.Spec.AgentConfig()
	if cfg.LLMProvider == nil {
		cfg.LLMProvider = f.deps.LLMProvider
	}
	if cfg.RunPolicy == nil && f.deps.RunPolicy != nil {
		cfg.RunPolicy = f.deps.RunPolicy
	}
	if cfg.Tools == nil {
		if f.deps.ToolRuntime != nil {
			cfg.Tools = f.deps.ToolRuntime.Registry()
		} else {
			cfg.Tools = f.deps.Tools
		}
	}
	if cfg.MaxTurns <= 0 && f.deps.DefaultMaxTurns > 0 {
		cfg.MaxTurns = f.deps.DefaultMaxTurns
	}
	prepared := &PreparedAgent{}
	if cfg.Sandbox == nil {
		if managed, ok := f.deps.SandboxRuntime.(ManagedSandboxRuntime); ok {
			binding, err := managed.Bind(req)
			if err != nil {
				return nil, err
			}
			cfg.Sandbox = binding.Sandbox
			prepared.Heartbeat = binding.Heartbeat
			prepared.Release = binding.Release
			prepared.HeartbeatInterval = binding.HeartbeatInterval
		} else if f.deps.SandboxRuntime != nil {
			sb, err := f.deps.SandboxRuntime.Resolve(req)
			if err != nil {
				return nil, err
			}
			cfg.Sandbox = sb
		} else if f.deps.SandboxProvider != nil && req.Features.Sandbox {
			sb, err := f.deps.SandboxProvider.Acquire()
			if err != nil {
				return nil, err
			}
			cfg.Sandbox = sb
		}
	}
	prepared.Agent = agent.New(cfg)
	return prepared, nil
}
