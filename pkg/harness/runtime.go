package harness

import "github.com/axeprpr/deerflow-go/pkg/agent"

// Runtime groups harness-managed runtime subsystems behind one boundary so
// compat/app layers do not own agent assembly, durable memory, or sandbox
// lifecycle separately.
type Runtime struct {
	factory         *Factory
	runner          *Runner
	memory          *MemoryRuntime
	sandboxProvider SandboxProvider
}

func NewRuntime(deps RuntimeDeps, memory *MemoryRuntime) *Runtime {
	factory := NewFactory(deps)
	return &Runtime{
		factory:         factory,
		runner:          NewRunner(factory),
		memory:          memory,
		sandboxProvider: deps.SandboxProvider,
	}
}

func (r *Runtime) NewAgent(req AgentRequest) (*agent.Agent, error) {
	if r == nil || r.factory == nil {
		return agent.New(req.Spec.AgentConfig()), nil
	}
	return r.factory.NewAgent(req)
}

func (r *Runtime) PrepareRun(req RunRequest) (*Execution, error) {
	if r == nil || r.runner == nil {
		return NewRunner(nil).Prepare(req)
	}
	return r.runner.Prepare(req)
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
