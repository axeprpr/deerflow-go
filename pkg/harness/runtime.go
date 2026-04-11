package harness

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/agent"
)

// Runtime groups harness-managed runtime subsystems behind one boundary so
// compat/app layers do not own agent assembly, durable memory, or sandbox
// lifecycle separately.
type Runtime struct {
	factory         *Factory
	runner          *Runner
	memory          *MemoryRuntime
	sandboxProvider SandboxProvider
	features        FeatureAssembly
	lifecycle       *LifecycleHooks
}

type RuntimeOption func(*Runtime)

func WithFeatureAssembly(features FeatureAssembly) RuntimeOption {
	return func(r *Runtime) {
		if r != nil {
			r.features = features
		}
	}
}

func WithLifecycle(hooks *LifecycleHooks) RuntimeOption {
	return func(r *Runtime) {
		if r != nil {
			r.lifecycle = hooks
		}
	}
}

func NewRuntime(deps RuntimeDeps, memory *MemoryRuntime, opts ...RuntimeOption) *Runtime {
	factory := NewFactory(deps)
	runtime := &Runtime{
		factory:         factory,
		runner:          NewRunner(factory),
		memory:          memory,
		sandboxProvider: deps.SandboxProvider,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(runtime)
		}
	}
	return runtime
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

func (r *Runtime) BindContext(ctx context.Context, spec ContextSpec) context.Context {
	return BindContext(ctx, spec)
}

func (r *Runtime) Features() FeatureAssembly {
	if r == nil {
		return FeatureAssembly{}
	}
	return r.features
}

func (r *Runtime) BeforeRun(ctx context.Context, state *RunState) error {
	if r == nil || r.lifecycle == nil {
		return nil
	}
	return r.lifecycle.Before(ctx, state)
}

func (r *Runtime) AfterRun(ctx context.Context, state *RunState, result *agent.RunResult) error {
	if r == nil || r.lifecycle == nil {
		return nil
	}
	return r.lifecycle.After(ctx, state, result)
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
