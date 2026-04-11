package langgraphcompat

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func (s *Server) HistorySummary(threadID string) string {
	return s.threadHistorySummary(threadID)
}

func (s *Server) PersistHistorySummary(threadID string, summary string) {
	s.setThreadHistorySummary(threadID, summary)
}

func (s *Server) CompactConversation(ctx context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) harness.SummarizationCompaction {
	compacted := s.compactConversationHistory(ctx, threadID, modelName, existingSummary, messages)
	return harness.SummarizationCompaction{
		Summary:  compacted.Summary,
		Messages: append([]models.Message(nil), compacted.Messages...),
		Changed:  compacted.Changed,
	}
}

func (s *Server) ResolveMemorySessionID(threadID string, agentName string) string {
	return deriveMemorySessionID(threadID, agentName)
}

func (s *Server) ComputeThreadTitle(ctx context.Context, threadID string, modelName string, messages []models.Message) string {
	return s.computeThreadTitle(ctx, threadID, modelName, messages)
}
