package harness

import "github.com/axeprpr/deerflow-go/pkg/agent"

// Runtime groups harness-managed runtime subsystems behind one boundary so
// compat/app layers do not own agent assembly, durable memory, or sandbox
// lifecycle separately.
type Runtime struct {
	factory         *Factory
	memory          *MemoryRuntime
	sandboxProvider SandboxProvider
}

func NewRuntime(deps RuntimeDeps, memory *MemoryRuntime) *Runtime {
	return &Runtime{
		factory:         NewFactory(deps),
		memory:          memory,
		sandboxProvider: deps.SandboxProvider,
	}
}

func (r *Runtime) NewAgent(req AgentRequest) (*agent.Agent, error) {
	if r == nil || r.factory == nil {
		return agent.New(req.Config), nil
	}
	return r.factory.NewAgent(req)
}

func (r *Runtime) Memory() *MemoryRuntime {
	if r == nil {
		return nil
	}
	return r.memory
}

func (r *Runtime) SandboxProvider() SandboxProvider {
	if r == nil {
		return nil
	}
	return r.sandboxProvider
}
