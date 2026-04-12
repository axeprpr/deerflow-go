package langgraphcompat

import (
	"context"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func (s *Server) runtimeProfileConfig(memoryRuntime *harness.MemoryRuntime) harnessruntime.ProfileConfig {
	return harnessruntime.ProfileConfig{
		Features: harnessruntime.FeatureConfig{
			ClarificationEnabled: s != nil && s.clarify != nil,
			MemoryEnabled:        memoryRuntime != nil && memoryRuntime.Enabled(),
			SummarizationEnabled: true,
			TitleEnabled:         false,
		},
		Lifecycle: harnessruntime.LifecycleConfig{
			SummaryMetadataKey:   historySummaryMetadataKey,
			MemorySessionKey:     "memory_session_id",
			InterruptMetadataKey: "clarification_interrupt",
			TitleMetadataKey:     "generated_title",
		},
		RunPolicy: harnessruntime.NewDefaultRunPolicy(),
	}
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
