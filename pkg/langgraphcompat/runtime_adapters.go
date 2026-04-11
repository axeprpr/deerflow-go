package langgraphcompat

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type runtimeConversationAdapter struct {
	server *Server
}

type runtimeMemoryAdapter struct {
	server *Server
}

type runtimeCompletionAdapter struct {
	server *Server
}

func (s *Server) runtimeConversationAdapter() runtimeConversationAdapter {
	return runtimeConversationAdapter{server: s}
}

func (s *Server) runtimeMemoryAdapter() runtimeMemoryAdapter {
	return runtimeMemoryAdapter{server: s}
}

func (s *Server) runtimeCompletionAdapter() runtimeCompletionAdapter {
	return runtimeCompletionAdapter{server: s}
}

func (a runtimeConversationAdapter) HistorySummary(threadID string) string {
	return a.server.threadHistorySummary(threadID)
}

func (a runtimeConversationAdapter) PersistHistorySummary(threadID string, summary string) {
	a.server.setThreadHistorySummary(threadID, summary)
}

func (a runtimeConversationAdapter) CompactConversation(ctx context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) harness.SummarizationCompaction {
	compacted := a.server.compactConversationHistory(ctx, threadID, modelName, existingSummary, messages)
	return harness.SummarizationCompaction{
		Summary:  compacted.Summary,
		Messages: append([]models.Message(nil), compacted.Messages...),
		Changed:  compacted.Changed,
	}
}

func (a runtimeConversationAdapter) ComputeThreadTitle(ctx context.Context, threadID string, modelName string, messages []models.Message) string {
	return a.server.computeThreadTitle(ctx, threadID, modelName, messages)
}

func (a runtimeMemoryAdapter) ResolveMemorySessionID(threadID string, agentName string) string {
	return deriveMemorySessionID(threadID, agentName)
}

func (a runtimeCompletionAdapter) SetThreadTitle(threadID string, title string) {
	a.server.setThreadMetadata(threadID, "title", title)
}

func (a runtimeCompletionAdapter) SetThreadInterrupts(threadID string, interrupts []any) {
	a.server.setThreadMetadata(threadID, "interrupts", interrupts)
}

func (a runtimeCompletionAdapter) ClearThreadInterrupts(threadID string) {
	a.server.deleteThreadMetadata(threadID, "interrupts")
}

func (a runtimeCompletionAdapter) MarkThreadStatus(threadID string, status string) {
	a.server.markThreadStatus(threadID, status)
}
