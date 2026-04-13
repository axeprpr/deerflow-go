package langgraphcompat

import (
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type serverRuntimeProfileBuilder struct {
	server         *Server
	memoryRuntime  *harness.MemoryRuntime
	toolRuntime    harness.ToolRuntime
	sandboxRuntime harness.SandboxRuntime
}

func (s *Server) runtimeProfileBuilder(memoryRuntime *harness.MemoryRuntime, toolRuntime harness.ToolRuntime, sandboxRuntime harness.SandboxRuntime) harness.ProfileBuilder {
	return serverRuntimeProfileBuilder{
		server:         s,
		memoryRuntime:  memoryRuntime,
		toolRuntime:    toolRuntime,
		sandboxRuntime: sandboxRuntime,
	}
}

func (b serverRuntimeProfileBuilder) BuildProfile() harness.RuntimeProfile {
	if b.server == nil {
		return harness.RuntimeProfile{}
	}
	config := b.server.runtimeProfileConfig(b.memoryRuntime)
	return config.BuildProfile(harnessruntime.ProfileProviders{
		ClarificationManager: b.server.clarify,
		ToolRuntime:          b.toolRuntime,
		SandboxRuntime:       b.sandboxRuntime,
		Lifecycle: harnessruntime.LifecycleProviders{
			MemoryRuntime:  b.memoryRuntime,
			Summarizer:     harnessruntime.NewSummarizer(b.server.runtimeConversationAdapter()),
			MemoryPlanner:  harnessruntime.NewMemoryScopePlanner(b.server.runtimeMemoryAdapter()),
			TitleGenerator: harnessruntime.NewTitleGenerator(b.server.runtimeConversationAdapter()),
		},
	})
}
