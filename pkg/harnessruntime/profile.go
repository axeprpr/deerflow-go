package harnessruntime

import (
	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type ProfileConfig struct {
	Mode      ExecutionMode
	Features  FeatureConfig
	Lifecycle LifecycleConfig
	RunPolicy *agent.RunPolicy
}

type ProfileProviders struct {
	ClarificationManager *clarification.Manager
	ToolRuntime          harness.ToolRuntime
	SandboxRuntime       harness.SandboxRuntime
	Lifecycle            LifecycleProviders
}

func (c ProfileConfig) BuildProfile(providers ProfileProviders) harness.RuntimeProfile {
	assembly := c.Features.BuildAssembly(providers.ClarificationManager)
	runPolicy := c.RunPolicy
	if runPolicy == nil {
		runPolicy = NewDefaultRunPolicy()
	}
	return harness.RuntimeProfile{
		RunPolicy:      runPolicy,
		ToolRuntime:    modeToolRuntime(c.Mode, providers.ToolRuntime),
		SandboxRuntime: modeSandboxRuntime(c.Mode, providers.SandboxRuntime),
		Features:       assembly,
		Lifecycle:      c.Lifecycle.BuildHooks(assembly, providers.Lifecycle),
	}
}
