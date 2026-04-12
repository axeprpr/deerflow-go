package langgraphcompat

import (
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type serverRuntimeProfileBuilder struct {
	server        *Server
	memoryRuntime *harness.MemoryRuntime
}

func (s *Server) runtimeProfileBuilder(memoryRuntime *harness.MemoryRuntime) harness.ProfileBuilder {
	return serverRuntimeProfileBuilder{
		server:        s,
		memoryRuntime: memoryRuntime,
	}
}

func (b serverRuntimeProfileBuilder) BuildProfile() harness.RuntimeProfile {
	if b.server == nil {
		return harness.RuntimeProfile{}
	}
	config := b.server.runtimeProfileConfig(b.memoryRuntime)
	return config.BuildProfile(harnessruntime.ProfileProviders{
		ClarificationManager: b.server.clarify,
		Lifecycle: harnessruntime.LifecycleProviders{
			MemoryRuntime:  b.memoryRuntime,
			Summarizer:     harnessruntime.NewSummarizer(b.server.runtimeConversationAdapter()),
			MemoryResolver: harnessruntime.NewMemorySessionResolver(b.server.runtimeMemoryAdapter()),
			TitleGenerator: harnessruntime.NewTitleGenerator(b.server.runtimeConversationAdapter()),
		},
	})
}
