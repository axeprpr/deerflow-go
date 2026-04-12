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
	if mode == ExecutionModeDefault || mode == ExecutionModeInteractive {
		return base
	}
	profile := base
	profile.ToolRuntime = modeToolRuntime(mode, base.ToolRuntime)
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
