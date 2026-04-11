package langgraphcompat

import "github.com/axeprpr/deerflow-go/pkg/harness"

type serverRuntimeFeatureBuilder struct {
	server        *Server
	memoryRuntime *harness.MemoryRuntime
}

func (s *Server) runtimeFeatureBuilder(memoryRuntime *harness.MemoryRuntime) harness.FeatureBuilder {
	return serverRuntimeFeatureBuilder{
		server:        s,
		memoryRuntime: memoryRuntime,
	}
}

func (b serverRuntimeFeatureBuilder) Build() harness.FeatureBundle {
	if b.server == nil {
		return harness.FeatureBundle{}
	}
	return harness.FeatureBundle{
		Assembly:  b.server.runtimeFeatureAssembly(b.memoryRuntime),
		Lifecycle: b.server.runtimeLifecycleHooks(b.memoryRuntime),
	}
}
