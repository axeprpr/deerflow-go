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
	config := b.server.runtimeFeatureConfig(b.memoryRuntime)
	assembly := config.BuildAssembly(b.server.clarify)
	return harness.FeatureBundle{
		Assembly:  assembly,
		Lifecycle: b.server.runtimeLifecycleHooks(b.memoryRuntime, assembly),
	}
}
