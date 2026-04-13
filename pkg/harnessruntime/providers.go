package harnessruntime

import (
	"context"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type SummaryLookup func(threadID string) string

type SummaryPersist func(threadID string, summary string)

type ConversationCompactor func(ctx context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) harness.SummarizationCompaction

type ConversationRuntime interface {
	HistorySummary(threadID string) string
	PersistHistorySummary(threadID string, summary string)
	CompactConversation(ctx context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) harness.SummarizationCompaction
}

type Summarizer struct {
	LoadSummary         SummaryLookup
	CompactConversation ConversationCompactor
	Persist             SummaryPersist
}

func NewSummarizer(runtime ConversationRuntime) Summarizer {
	if runtime == nil {
		return Summarizer{}
	}
	return Summarizer{
		LoadSummary:         runtime.HistorySummary,
		CompactConversation: runtime.CompactConversation,
		Persist:             runtime.PersistHistorySummary,
	}
}

func (s Summarizer) Compact(ctx context.Context, state *harness.RunState) (harness.SummarizationCompaction, error) {
	if state == nil || len(state.Messages) == 0 {
		return harness.SummarizationCompaction{Messages: append([]models.Message(nil), stateMessages(state)...)}, nil
	}
	if s.CompactConversation == nil {
		return harness.SummarizationCompaction{Messages: append([]models.Message(nil), state.Messages...)}, nil
	}
	existingSummary := ""
	if s.LoadSummary != nil {
		existingSummary = strings.TrimSpace(s.LoadSummary(state.ThreadID))
	}
	compacted := s.CompactConversation(ctx, state.ThreadID, state.Model, existingSummary, state.Messages)
	return harness.SummarizationCompaction{
		Summary:  strings.TrimSpace(compacted.Summary),
		Messages: append([]models.Message(nil), compacted.Messages...),
		Changed:  compacted.Changed,
	}, nil
}

func (s Summarizer) PersistSummary(threadID string, summary string) {
	if s.Persist == nil {
		return
	}
	s.Persist(threadID, summary)
}

type MemorySessionIDResolver func(threadID string, agentName string) string

type SessionRuntime interface {
	ResolveMemorySessionID(threadID string, agentName string) string
}

type MemoryScopeRuntime interface {
	ResolveMemoryScope(threadID string, agentName string) pkgmemory.Scope
}

type MemoryScopePlanRuntime interface {
	PlanMemoryScopes(threadID string, agentName string) harness.MemoryScopePlan
}

type MemorySessionResolver struct {
	Resolve MemorySessionIDResolver
}

type MemoryScopeResolver struct {
	Resolve func(threadID string, agentName string) pkgmemory.Scope
}

type MemoryScopePlanner struct {
	Plan func(threadID string, agentName string) harness.MemoryScopePlan
}

func NewMemorySessionResolver(runtime SessionRuntime) MemorySessionResolver {
	if runtime == nil {
		return MemorySessionResolver{}
	}
	return MemorySessionResolver{Resolve: runtime.ResolveMemorySessionID}
}

func NewMemoryScopeResolver(runtime MemoryScopeRuntime) MemoryScopeResolver {
	if runtime == nil {
		return MemoryScopeResolver{}
	}
	return MemoryScopeResolver{Resolve: runtime.ResolveMemoryScope}
}

func NewMemoryScopePlanner(runtime MemoryScopePlanRuntime) MemoryScopePlanner {
	if runtime == nil {
		return MemoryScopePlanner{}
	}
	return MemoryScopePlanner{Plan: runtime.PlanMemoryScopes}
}

func (r MemorySessionResolver) ResolveMemorySession(state *harness.RunState) string {
	if state == nil || r.Resolve == nil {
		return ""
	}
	return strings.TrimSpace(r.Resolve(state.ThreadID, state.AgentName))
}

func (r MemoryScopeResolver) ResolveMemoryScope(state *harness.RunState) pkgmemory.Scope {
	if state == nil || r.Resolve == nil {
		return pkgmemory.Scope{}
	}
	return r.Resolve(state.ThreadID, state.AgentName).Normalized()
}

func (r MemoryScopePlanner) PlanMemoryScopes(state *harness.RunState) harness.MemoryScopePlan {
	if state == nil || r.Plan == nil {
		return harness.MemoryScopePlan{}
	}
	return harness.NormalizeMemoryScopePlan(r.Plan(state.ThreadID, state.AgentName))
}

type TitleGeneratorFunc func(ctx context.Context, threadID string, modelName string, messages []models.Message) string

type TitleRuntime interface {
	ComputeThreadTitle(ctx context.Context, threadID string, modelName string, messages []models.Message) string
}

type TitleGenerator struct {
	Generate TitleGeneratorFunc
}

func NewTitleGenerator(runtime TitleRuntime) TitleGenerator {
	if runtime == nil {
		return TitleGenerator{}
	}
	return TitleGenerator{Generate: runtime.ComputeThreadTitle}
}

func (g TitleGenerator) GenerateTitle(ctx context.Context, state *harness.RunState, result *agent.RunResult) string {
	if state == nil || result == nil || g.Generate == nil {
		return ""
	}
	return strings.TrimSpace(g.Generate(ctx, state.ThreadID, state.Model, result.Messages))
}

func stateMessages(state *harness.RunState) []models.Message {
	if state == nil {
		return nil
	}
	return state.Messages
}
