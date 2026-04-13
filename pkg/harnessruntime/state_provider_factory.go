package harnessruntime

type RuntimeStatePlaneProvidersFactory interface {
	Build(RuntimeNodeConfig) RuntimeStatePlaneProviders
}

type RuntimeStatePlaneProvidersFactoryFunc func(RuntimeNodeConfig) RuntimeStatePlaneProviders

func (f RuntimeStatePlaneProvidersFactoryFunc) Build(config RuntimeNodeConfig) RuntimeStatePlaneProviders {
	return f(config)
}

func DefaultRuntimeStatePlaneProvidersFactory() RuntimeStatePlaneProvidersFactory {
	return RuntimeStatePlaneProvidersFactoryFunc(func(RuntimeNodeConfig) RuntimeStatePlaneProviders {
		return DefaultRuntimeStatePlaneProviders()
	})
}
