package langgraphcompat

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type runtimeAdapter struct {
	server *Server
}

func (s *Server) runtimeAdapter() runtimeAdapter {
	return runtimeAdapter{server: s}
}

func (a runtimeAdapter) HistorySummary(threadID string) string {
	return a.server.threadHistorySummary(threadID)
}

func (a runtimeAdapter) PersistHistorySummary(threadID string, summary string) {
	a.server.setThreadHistorySummary(threadID, summary)
}

func (a runtimeAdapter) CompactConversation(ctx context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) harness.SummarizationCompaction {
	compacted := a.server.compactConversationHistory(ctx, threadID, modelName, existingSummary, messages)
	return harness.SummarizationCompaction{
		Summary:  compacted.Summary,
		Messages: append([]models.Message(nil), compacted.Messages...),
		Changed:  compacted.Changed,
	}
}

func (a runtimeAdapter) ResolveMemorySessionID(threadID string, agentName string) string {
	return deriveMemorySessionID(threadID, agentName)
}

func (a runtimeAdapter) ComputeThreadTitle(ctx context.Context, threadID string, modelName string, messages []models.Message) string {
	return a.server.computeThreadTitle(ctx, threadID, modelName, messages)
}

func (a runtimeAdapter) SetThreadTitle(threadID string, title string) {
	a.server.setThreadMetadata(threadID, "title", title)
}

func (a runtimeAdapter) SetThreadInterrupts(threadID string, interrupts []any) {
	a.server.setThreadMetadata(threadID, "interrupts", interrupts)
}

func (a runtimeAdapter) ClearThreadInterrupts(threadID string) {
	a.server.deleteThreadMetadata(threadID, "interrupts")
}

func (a runtimeAdapter) MarkThreadStatus(threadID string, status string) {
	a.server.markThreadStatus(threadID, status)
}
