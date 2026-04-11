package harnessruntime

import (
	"context"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type SummaryLookup func(threadID string) string

type SummaryPersist func(threadID string, summary string)

type ConversationCompactor func(ctx context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) harness.SummarizationCompaction

type Summarizer struct {
	LoadSummary         SummaryLookup
	CompactConversation ConversationCompactor
	Persist             SummaryPersist
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

type MemorySessionResolver struct {
	Resolve MemorySessionIDResolver
}

func (r MemorySessionResolver) ResolveMemorySession(state *harness.RunState) string {
	if state == nil || r.Resolve == nil {
		return ""
	}
	return strings.TrimSpace(r.Resolve(state.ThreadID, state.AgentName))
}

type TitleGeneratorFunc func(ctx context.Context, threadID string, modelName string, messages []models.Message) string

type TitleGenerator struct {
	Generate TitleGeneratorFunc
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
