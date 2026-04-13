package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

func modeToolRuntime(mode ExecutionMode, base harness.ToolRuntime) harness.ToolRuntime {
	if base == nil {
		return nil
	}
	if mode == ExecutionModeDefault || mode == ExecutionModeInteractive {
		return base
	}

	registry := base.Registry()
	if registry == nil {
		return base
	}

	denylist := map[string]struct{}{}
	switch mode {
	case ExecutionModeBackground:
		denylist["ask_clarification"] = struct{}{}
		denylist["task"] = struct{}{}
	case ExecutionModeStrict:
		denylist["task"] = struct{}{}
	default:
		return base
	}

	allowed := make([]string, 0, len(registry.List()))
	for _, tool := range registry.List() {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		if _, denied := denylist[name]; denied {
			continue
		}
		allowed = append(allowed, name)
	}

	restricted := registry.Restrict(allowed)
	return harness.NewStaticToolRuntime(restricted, base.DeferredTools(), nil)
}

type modeSandboxRuntimeWrapper struct {
	base   harness.SandboxRuntime
	policy harness.SandboxPolicy
}

func (r modeSandboxRuntimeWrapper) Provider() harness.SandboxProvider {
	if r.base == nil {
		return nil
	}
	return r.base.Provider()
}

func (r modeSandboxRuntimeWrapper) Resolve(req harness.AgentRequest) (sandbox.Session, error) {
	binding, err := r.Bind(req)
	if err != nil {
		return nil, err
	}
	return binding.Sandbox, nil
}

func (r modeSandboxRuntimeWrapper) Bind(req harness.AgentRequest) (harness.SandboxBinding, error) {
	if r.base == nil {
		return harness.SandboxBinding{}, nil
	}
	if r.policy != nil && !r.policy.Enabled(req) {
		return harness.SandboxBinding{}, nil
	}
	if r.policy != nil {
		req.Features.Sandbox = true
	}
	if managed, ok := r.base.(harness.ManagedSandboxRuntime); ok {
		return managed.Bind(req)
	}
	sb, err := r.base.Resolve(req)
	if err != nil {
		return harness.SandboxBinding{}, err
	}
	return harness.SandboxBinding{Sandbox: sb}, nil
}

func (r modeSandboxRuntimeWrapper) Close() error {
	if r.base == nil {
		return nil
	}
	return r.base.Close()
}

func modeSandboxRuntime(mode ExecutionMode, base harness.SandboxRuntime) harness.SandboxRuntime {
	if base == nil {
		return nil
	}
	switch mode {
	case ExecutionModeStrict:
		return modeSandboxRuntimeWrapper{
			base:   base,
			policy: harness.AlwaysSandboxPolicy{},
		}
	default:
		return base
	}
}
