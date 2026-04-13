package harnessruntime

func DefaultRuntimeStatePlaneProvidersForConfig(config RuntimeNodeConfig) RuntimeStatePlaneProviders {
	providers := DefaultRuntimeStatePlaneProviders()
	switch normalizeRuntimeStateProviderMode(config.State.Provider) {
	case RuntimeStateProviderModeIsolated:
		providers.Plane = nil
		providers.DisableSharedPlane = true
	case RuntimeStateProviderModeSharedSQLite:
		providers.Plane = RuntimeStatePlaneFactoryFunc(func(config RuntimeNodeConfig) RuntimeStatePlane {
			plane, ok := providers.Backends.Resolve(RuntimeStateStoreBackendSQLite).BuildPlane(config)
			if !ok {
				return RuntimeStatePlane{}
			}
			return plane
		})
	}
	return providers
}

func normalizeRuntimeStateProviderMode(value RuntimeStateProviderMode) RuntimeStateProviderMode {
	switch value {
	case RuntimeStateProviderModeIsolated:
		return RuntimeStateProviderModeIsolated
	case RuntimeStateProviderModeSharedSQLite:
		return RuntimeStateProviderModeSharedSQLite
	default:
		return RuntimeStateProviderModeAuto
	}
}
