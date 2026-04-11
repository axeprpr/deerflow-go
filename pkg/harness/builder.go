package harness

type FeatureBundle struct {
	Assembly  FeatureAssembly
	Lifecycle *LifecycleHooks
}

type FeatureBuilder interface {
	Build() FeatureBundle
}

type StaticFeatureBuilder struct {
	bundle FeatureBundle
}

func NewStaticFeatureBuilder(assembly FeatureAssembly, lifecycle *LifecycleHooks) StaticFeatureBuilder {
	return StaticFeatureBuilder{
		bundle: FeatureBundle{
			Assembly:  assembly,
			Lifecycle: lifecycle,
		},
	}
}

func (b StaticFeatureBuilder) Build() FeatureBundle {
	return b.bundle
}
