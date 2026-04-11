package langgraphcompat

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
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

type runtimePreflightAdapter struct {
	server *Server
}

type runtimeRunStateAdapter struct {
	server *Server
}

type runtimeContextAdapter struct {
	server *Server
}

type runtimeCoordinationAdapter struct {
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

func (s *Server) runtimePreflightAdapter() runtimePreflightAdapter {
	return runtimePreflightAdapter{server: s}
}

func (s *Server) runtimeRunStateAdapter() runtimeRunStateAdapter {
	return runtimeRunStateAdapter{server: s}
}

func (s *Server) runtimeContextAdapter() runtimeContextAdapter {
	return runtimeContextAdapter{server: s}
}

func (s *Server) runtimeCoordinationAdapter() runtimeCoordinationAdapter {
	return runtimeCoordinationAdapter{server: s}
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

func (a runtimePreflightAdapter) PrepareSession(threadID string) harnessruntime.SessionSnapshot {
	session := a.server.ensureSession(threadID, nil)
	if session.PresentFiles != nil {
		session.PresentFiles.Clear()
	}
	a.server.sessionsMu.RLock()
	existingMessages := append([]models.Message(nil), session.Messages...)
	a.server.sessionsMu.RUnlock()
	return harnessruntime.SessionSnapshot{
		PresentFiles:     session.PresentFiles,
		ExistingMessages: existingMessages,
	}
}

func (a runtimePreflightAdapter) MarkThreadStatus(threadID string, status string) {
	a.server.markThreadStatus(threadID, status)
}

func (a runtimePreflightAdapter) SetThreadMetadata(threadID string, key string, value any) {
	a.server.setThreadMetadata(threadID, key, value)
}

func (a runtimePreflightAdapter) ClearThreadMetadata(threadID string, key string) {
	a.server.deleteThreadMetadata(threadID, key)
}

func (a runtimePreflightAdapter) SaveRunRecord(record harnessruntime.RunRecord) {
	a.server.saveRun(runFromRecord(record))
}

func (a runtimeRunStateAdapter) SaveRunRecord(record harnessruntime.RunRecord) {
	a.server.saveRun(runFromRecord(record))
}

func (a runtimeRunStateAdapter) MarkThreadStatus(threadID string, status string) {
	a.server.markThreadStatus(threadID, status)
}

func (a runtimeContextAdapter) ClarificationManager() *clarification.Manager {
	return a.server.clarify
}

func (a runtimeContextAdapter) SkillPaths() any {
	return a.server.runtimeSkillPaths()
}

func (a runtimeCoordinationAdapter) LoadRunRecord(runID string) (harnessruntime.RunRecord, bool) {
	run := a.server.getRun(runID)
	if run == nil {
		return harnessruntime.RunRecord{}, false
	}
	return runRecordFromRun(run), true
}

func (a runtimeCoordinationAdapter) CancelRun(runID string) bool {
	return a.server.cancelActiveRun(runID)
}
