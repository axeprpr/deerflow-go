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

func (s *Server) SetThreadTitle(threadID string, title string) {
	s.setThreadMetadata(threadID, "title", title)
}

func (s *Server) SetThreadInterrupts(threadID string, interrupts []any) {
	s.setThreadMetadata(threadID, "interrupts", interrupts)
}

func (s *Server) ClearThreadInterrupts(threadID string) {
	s.deleteThreadMetadata(threadID, "interrupts")
}

func (s *Server) MarkThreadStatus(threadID string, status string) {
	s.markThreadStatus(threadID, status)
}
