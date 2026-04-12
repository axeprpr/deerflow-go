package harnessruntime

import (
	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type ProfileConfig struct {
	Features  FeatureConfig
	Lifecycle LifecycleConfig
	RunPolicy *agent.RunPolicy
}

type ProfileProviders struct {
	ClarificationManager *clarification.Manager
	Lifecycle            LifecycleProviders
}

func (c ProfileConfig) BuildProfile(providers ProfileProviders) harness.RuntimeProfile {
	assembly := c.Features.BuildAssembly(providers.ClarificationManager)
	runPolicy := c.RunPolicy
	if runPolicy == nil {
		runPolicy = NewDefaultRunPolicy()
	}
	return harness.RuntimeProfile{
		RunPolicy: runPolicy,
		Features:  assembly,
		Lifecycle: c.Lifecycle.BuildHooks(assembly, providers.Lifecycle),
	}
}
