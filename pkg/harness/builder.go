package harness

import "github.com/axeprpr/deerflow-go/pkg/agent"

type FeatureBundle struct {
	Assembly  FeatureAssembly
	Lifecycle *LifecycleHooks
}

type RuntimeProfile struct {
	RunPolicy      *agent.RunPolicy
	ToolRuntime    ToolRuntime
	SandboxRuntime SandboxRuntime
	Features       FeatureAssembly
	Lifecycle      *LifecycleHooks
}

type FeatureBuilder interface {
	Build() FeatureBundle
}

type ProfileBuilder interface {
	BuildProfile() RuntimeProfile
}

type StaticFeatureBuilder struct {
	bundle FeatureBundle
}

type StaticProfileBuilder struct {
	profile RuntimeProfile
}

func NewStaticFeatureBuilder(assembly FeatureAssembly, lifecycle *LifecycleHooks) StaticFeatureBuilder {
	return StaticFeatureBuilder{
		bundle: FeatureBundle{
			Assembly:  assembly,
			Lifecycle: lifecycle,
		},
	}
}

func NewStaticProfileBuilder(profile RuntimeProfile) StaticProfileBuilder {
	return StaticProfileBuilder{profile: profile}
}

func (b StaticFeatureBuilder) Build() FeatureBundle {
	return b.bundle
}

func (b StaticProfileBuilder) BuildProfile() RuntimeProfile {
	return b.profile
}
