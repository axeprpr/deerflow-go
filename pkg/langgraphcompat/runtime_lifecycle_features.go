package langgraphcompat

import (
	"context"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type runtimeSummarizer struct {
	server *Server
}

func (s runtimeSummarizer) Compact(ctx context.Context, state *harness.RunState) (harness.SummarizationCompaction, error) {
	if s.server == nil || state == nil || len(state.Messages) == 0 {
		return harness.SummarizationCompaction{Messages: append([]models.Message(nil), state.Messages...)}, nil
	}
	existingSummary := s.server.threadHistorySummary(state.ThreadID)
	compacted := s.server.compactConversationHistory(ctx, state.ThreadID, state.Model, existingSummary, state.Messages)
	return harness.SummarizationCompaction{
		Summary:  compacted.Summary,
		Messages: compacted.Messages,
		Changed:  compacted.Changed,
	}, nil
}

func (s runtimeSummarizer) PersistSummary(threadID string, summary string) {
	if s.server == nil {
		return
	}
	s.server.setThreadHistorySummary(threadID, summary)
}

type runtimeMemoryResolver struct{}

func (runtimeMemoryResolver) ResolveMemorySession(state *harness.RunState) string {
	if state == nil {
		return ""
	}
	return deriveMemorySessionID(state.ThreadID, state.AgentName)
}

type runtimeTitleGenerator struct {
	server *Server
}

func (g runtimeTitleGenerator) GenerateTitle(ctx context.Context, state *harness.RunState, result *agent.RunResult) string {
	if g.server == nil || state == nil || result == nil {
		return ""
	}
	return g.server.computeThreadTitle(ctx, state.ThreadID, state.Model, result.Messages)
}

func (s *Server) runtimeFeatureAssembly(memoryRuntime *harness.MemoryRuntime) harness.FeatureAssembly {
	return harness.FeatureAssembly{
		Clarification: harness.ClarificationFeature{
			Enabled: s != nil && s.clarify != nil,
			Manager: s.clarify,
		},
		Memory: harness.MemoryFeature{
			Enabled: memoryRuntime != nil && memoryRuntime.Enabled(),
		},
		Summarization: harness.SummarizationFeature{
			Enabled: true,
		},
		Title: harness.TitleFeature{
			Enabled: false,
		},
	}
}

func (s *Server) runtimeLifecycleHooks(memoryRuntime *harness.MemoryRuntime) *harness.LifecycleHooks {
	features := s.runtimeFeatureAssembly(memoryRuntime)

	var titleHooks *harness.LifecycleHooks
	if features.Title.Enabled {
		titleHooks = harness.TitleLifecycleHooksWithGenerator(runtimeTitleGenerator{server: s}, "generated_title")
	}

	return harness.MergeLifecycleHooks(
		harness.SummarizationLifecycleHooksWithSummarizer(runtimeSummarizer{server: s}, historySummaryMetadataKey),
		harness.MemoryLifecycleHooksWithResolver(memoryRuntime, runtimeMemoryResolver{}, "memory_session_id"),
		harness.ClarificationLifecycleHooks(harness.ClarificationLifecycleConfig{
			InterruptMetadataKey: "clarification_interrupt",
		}),
		titleHooks,
	)
}

func (s *Server) beforeRunSummarizationFeature(ctx context.Context, state *harness.RunState) error {
	if s == nil || state == nil || len(state.Messages) == 0 {
		return nil
	}
	existingSummary := s.threadHistorySummary(state.ThreadID)
	compacted := s.compactConversationHistory(ctx, state.ThreadID, state.Model, existingSummary, state.Messages)
	state.Messages = append([]models.Message(nil), compacted.Messages...)
	if compacted.Changed {
		if summary := strings.TrimSpace(compacted.Summary); summary != "" {
			state.Metadata[historySummaryMetadataKey] = summary
		}
	}
	return nil
}

func (s *Server) beforeRunMemoryFeature(ctx context.Context, memoryRuntime *harness.MemoryRuntime, state *harness.RunState) error {
	if s == nil || state == nil || memoryRuntime == nil || !memoryRuntime.Enabled() {
		return nil
	}
	sessionID := deriveMemorySessionID(state.ThreadID, state.AgentName)
	state.Metadata["memory_session_id"] = sessionID
	injected := strings.TrimSpace(memoryRuntime.Inject(ctx, sessionID, state.Spec.SystemPrompt))
	if injected == "" {
		return nil
	}
	if prompt := strings.TrimSpace(state.Spec.SystemPrompt); prompt != "" {
		state.Spec.SystemPrompt = strings.TrimSpace(prompt + "\n\n" + injected)
		return nil
	}
	state.Spec.SystemPrompt = injected
	return nil
}

func (s *Server) afterRunSummarizationFeature(state *harness.RunState) {
	if s == nil || state == nil || len(state.Metadata) == 0 {
		return
	}
	summary := strings.TrimSpace(stringValue(state.Metadata[historySummaryMetadataKey]))
	if summary == "" {
		return
	}
	s.setThreadHistorySummary(state.ThreadID, summary)
}

func (s *Server) afterRunMemoryFeature(memoryRuntime *harness.MemoryRuntime, state *harness.RunState, result *agent.RunResult) {
	if s == nil || state == nil || result == nil || memoryRuntime == nil || !memoryRuntime.Enabled() {
		return
	}
	sessionID := strings.TrimSpace(stringValue(state.Metadata["memory_session_id"]))
	if sessionID == "" {
		sessionID = deriveMemorySessionID(state.ThreadID, state.AgentName)
	}
	if sessionID == "" || len(result.Messages) == 0 {
		return
	}
	memoryRuntime.ScheduleUpdate(sessionID, result.Messages)
}

func (s *Server) afterRunClarificationFeature(state *harness.RunState, result *agent.RunResult) {
	if state == nil || result == nil {
		return
	}
	if interrupt := harness.ClarificationInterruptFromMessages(result.Messages); interrupt != nil {
		state.Metadata["clarification_interrupt"] = interrupt
	}
}

func (s *Server) afterRunTitleFeature(ctx context.Context, state *harness.RunState, result *agent.RunResult) {
	if s == nil || state == nil || result == nil {
		return
	}
	title := s.computeThreadTitle(ctx, state.ThreadID, state.Model, result.Messages)
	if strings.TrimSpace(title) == "" {
		return
	}
	state.Metadata["generated_title"] = title
}
