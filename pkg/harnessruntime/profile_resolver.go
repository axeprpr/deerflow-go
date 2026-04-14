package harnessruntime

import (
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type ModeProfileResolver struct{}

func NewModeProfileResolver() ModeProfileResolver {
	return ModeProfileResolver{}
}

func (ModeProfileResolver) ResolveProfile(base harness.RuntimeProfile, req harness.AgentRequest) harness.RuntimeProfile {
	mode := parseExecutionMode(req.Spec.ExecutionMode)
	profile := base
	profile.ToolRuntime = featureToolRuntime(req, modeToolRuntime(mode, base.ToolRuntime))
	profile.SandboxRuntime = modeSandboxRuntime(mode, base.SandboxRuntime)
	return profile
}

func parseExecutionMode(raw string) ExecutionMode {
	switch ExecutionMode(strings.ToLower(strings.TrimSpace(raw))) {
	case ExecutionModeInteractive:
		return ExecutionModeInteractive
	case ExecutionModeBackground:
		return ExecutionModeBackground
	case ExecutionModeStrict:
		return ExecutionModeStrict
	default:
		return ExecutionModeDefault
	}
}

func featureToolRuntime(req harness.AgentRequest, base harness.ToolRuntime) harness.ToolRuntime {
	if base == nil {
		return nil
	}
	if req.Features.Subagent {
		return base
	}

	registry := base.Registry()
	if registry == nil || registry.Get("task") == nil {
		return base
	}

	allowed := make([]string, 0, len(registry.List()))
	for _, tool := range registry.List() {
		name := strings.TrimSpace(tool.Name)
		if name == "" || name == "task" {
			continue
		}
		allowed = append(allowed, name)
	}
	return harness.NewStaticToolRuntime(registry.Restrict(allowed), base.DeferredTools(), nil)
}
