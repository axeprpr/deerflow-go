package harness

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/agent"
)

// Runtime groups harness-managed runtime subsystems behind one boundary so
// compat/app layers do not own agent assembly, durable memory, or sandbox
// lifecycle separately.
type Runtime struct {
	factory        *Factory
	runner         *Runner
	memory         *MemoryRuntime
	sandboxRuntime SandboxRuntime
	toolRuntime    ToolRuntime
	profileResolver ProfileResolver
	profile        RuntimeProfile
	features       FeatureAssembly
	lifecycle      *LifecycleHooks
}

type RuntimeOption func(*Runtime)

func WithFeatureAssembly(features FeatureAssembly) RuntimeOption {
	return func(r *Runtime) {
		if r != nil {
			r.features = features
			r.profile.Features = features
		}
	}
}

func WithLifecycle(hooks *LifecycleHooks) RuntimeOption {
	return func(r *Runtime) {
		if r != nil {
			r.lifecycle = hooks
			r.profile.Lifecycle = hooks
		}
	}
}

func WithFeatureBuilder(builder FeatureBuilder) RuntimeOption {
	return func(r *Runtime) {
		if r == nil || builder == nil {
			return
		}
		bundle := builder.Build()
		r.features = bundle.Assembly
		r.lifecycle = bundle.Lifecycle
		r.profile.Features = bundle.Assembly
		r.profile.Lifecycle = bundle.Lifecycle
	}
}

func WithProfileBuilder(builder ProfileBuilder) RuntimeOption {
	return func(r *Runtime) {
		if r == nil || builder == nil {
			return
		}
		r.applyProfile(builder.BuildProfile())
	}
}

func NewRuntime(deps RuntimeDeps, memory *MemoryRuntime, opts ...RuntimeOption) *Runtime {
	sandboxRuntime := deps.SandboxRuntime
	if sandboxRuntime == nil && deps.SandboxProvider != nil {
		sandboxRuntime = NewStaticSandboxRuntime(deps.SandboxProvider, FeatureSandboxPolicy{})
	}
	factory := NewFactory(deps)
	runtime := &Runtime{
		factory:        factory,
		runner:         NewRunner(factory),
		memory:         memory,
		sandboxRuntime: sandboxRuntime,
		toolRuntime:    deps.ToolRuntime,
		profileResolver: deps.ProfileResolver,
		profile: RuntimeProfile{
			RunPolicy:      deps.RunPolicy,
			ToolRuntime:    deps.ToolRuntime,
			SandboxRuntime: sandboxRuntime,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(runtime)
		}
	}
	return runtime
}

func (r *Runtime) applyProfile(profile RuntimeProfile) {
	if r == nil {
		return
	}
	if profile.ToolRuntime == nil {
		profile.ToolRuntime = r.toolRuntime
	}
	if profile.SandboxRuntime == nil {
		profile.SandboxRuntime = r.sandboxRuntime
	}
	r.profile = profile
	r.toolRuntime = profile.ToolRuntime
	r.sandboxRuntime = profile.SandboxRuntime
	r.features = profile.Features
	r.lifecycle = profile.Lifecycle
	if r.factory != nil {
		if profile.RunPolicy != nil {
			r.factory.deps.RunPolicy = profile.RunPolicy
		}
		r.factory.deps.ToolRuntime = profile.ToolRuntime
		r.factory.deps.SandboxRuntime = profile.SandboxRuntime
		if profile.SandboxRuntime != nil {
			r.factory.deps.SandboxProvider = profile.SandboxRuntime.Provider()
		}
	}
}

func (r *Runtime) NewAgent(req AgentRequest) (*agent.Agent, error) {
	if r == nil || r.factory == nil {
		return agent.New(req.Spec.AgentConfig()), nil
	}
	if r.profileResolver == nil {
		return r.factory.NewAgent(req)
	}
	profile := r.profileResolver.ResolveProfile(r.profile, req)
	deps := r.factory.deps
	if profile.RunPolicy != nil {
		deps.RunPolicy = profile.RunPolicy
	}
	if profile.ToolRuntime != nil {
		deps.ToolRuntime = profile.ToolRuntime
	}
	if profile.SandboxRuntime != nil {
		deps.SandboxRuntime = profile.SandboxRuntime
		deps.SandboxProvider = profile.SandboxRuntime.Provider()
	}
	return NewFactory(deps).NewAgent(req)
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
	return r.profile.Features
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

func (r *Runtime) Profile() RuntimeProfile {
	if r == nil {
		return RuntimeProfile{}
	}
	return r.profile
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
	if r.sandboxRuntime == nil {
		return nil
	}
	return r.sandboxRuntime.Provider()
}

func (r *Runtime) SandboxRuntime() SandboxRuntime {
	if r == nil {
		return nil
	}
	return r.sandboxRuntime
}

func (r *Runtime) ToolRuntime() ToolRuntime {
	if r == nil {
		return nil
	}
	return r.toolRuntime
}
