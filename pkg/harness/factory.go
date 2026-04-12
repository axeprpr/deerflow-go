package harness

import (
	"github.com/axeprpr/deerflow-go/pkg/agent"
)

// Factory owns runtime assembly. Compat and HTTP layers should depend on this
// boundary instead of constructing agents directly.
type Factory struct {
	deps RuntimeDeps
}

func NewFactory(deps RuntimeDeps) *Factory {
	return &Factory{deps: deps}
}

func (f *Factory) NewAgent(req AgentRequest) (*agent.Agent, error) {
	cfg := req.Spec.AgentConfig()
	if cfg.LLMProvider == nil {
		cfg.LLMProvider = f.deps.LLMProvider
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
	if cfg.Sandbox == nil && f.deps.SandboxProvider != nil && req.Features.Sandbox {
		sb, err := f.deps.SandboxProvider.Acquire()
		if err != nil {
			return nil, err
		}
		cfg.Sandbox = sb
	}
	return agent.New(cfg), nil
}
