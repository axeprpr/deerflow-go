package langgraphcompat

import (
	"context"
	"strings"
	"sync"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
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

type runtimeQueryAdapter struct {
	server *Server
}

type runtimeEventAdapter struct {
	server *Server
}

type runtimeSnapshotAdapter struct {
	server *Server
}

type runtimeWorkerSpecAdapter struct {
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

func (s *Server) runtimeQueryAdapter() runtimeQueryAdapter {
	return runtimeQueryAdapter{server: s}
}

func (s *Server) runtimeEventAdapter() runtimeEventAdapter {
	return runtimeEventAdapter{server: s}
}

func (s *Server) runtimeSnapshotAdapter() runtimeSnapshotAdapter {
	return runtimeSnapshotAdapter{server: s}
}

func (s *Server) runtimeWorkerSpecAdapter() runtimeWorkerSpecAdapter {
	return runtimeWorkerSpecAdapter{server: s}
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
	return a.PlanMemoryScopes(threadID, agentName).Primary.Key()
}

func (a runtimeMemoryAdapter) ResolveMemoryScope(threadID string, agentName string) pkgmemory.Scope {
	return a.PlanMemoryScopes(threadID, agentName).Primary
}

func (a runtimeMemoryAdapter) PlanMemoryScopes(threadID string, agentName string) harness.MemoryScopePlan {
	hints := harnessruntime.MemoryScopeHints{
		ThreadID:  threadID,
		AgentName: agentName,
	}
	if store := a.server.ensureThreadStateStore(); store != nil {
		if state, ok := store.LoadThreadRuntimeState(threadID); ok {
			hints.UserID = strings.TrimSpace(stringValue(state.Metadata["memory_user_id"]))
			hints.GroupID = strings.TrimSpace(stringValue(state.Metadata["memory_group_id"]))
			hints.Namespace = strings.TrimSpace(stringValue(state.Metadata["memory_namespace"]))
		}
	}
	return harnessruntime.NewMemoryScopeService(nil).Plan(hints)
}

func (a runtimeCompletionAdapter) SetThreadTitle(threadID string, title string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.SetThreadMetadata(threadID, "title", title)
	}
}

func (a runtimeCompletionAdapter) SetThreadInterrupts(threadID string, interrupts []any) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.SetThreadMetadata(threadID, "interrupts", interrupts)
	}
}

func (a runtimeCompletionAdapter) ClearThreadInterrupts(threadID string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.ClearThreadMetadata(threadID, "interrupts")
	}
}

func (a runtimeCompletionAdapter) MarkThreadStatus(threadID string, status string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.MarkThreadStatus(threadID, status)
	}
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
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.MarkThreadStatus(threadID, status)
	}
}

func (a runtimePreflightAdapter) SetThreadMetadata(threadID string, key string, value any) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.SetThreadMetadata(threadID, key, value)
	}
}

func (a runtimePreflightAdapter) ClearThreadMetadata(threadID string, key string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.ClearThreadMetadata(threadID, key)
	}
}

func (a runtimePreflightAdapter) SaveRunRecord(record harnessruntime.RunRecord) {
	harnessruntime.NewSnapshotStoreService(a.server.ensureSnapshotStore()).SaveRecord(record)
}

func (a runtimeRunStateAdapter) SaveRunRecord(record harnessruntime.RunRecord) {
	harnessruntime.NewSnapshotStoreService(a.server.ensureSnapshotStore()).SaveRecord(record)
}

func (a runtimeRunStateAdapter) MarkThreadStatus(threadID string, status string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.MarkThreadStatus(threadID, status)
	}
}

func (a runtimeContextAdapter) ClarificationManager() *clarification.Manager {
	return a.server.clarify
}

func (a runtimeContextAdapter) SkillPaths() any {
	return a.server.runtimeSkillPaths()
}

func (a runtimeCoordinationAdapter) LoadRunRecord(runID string) (harnessruntime.RunRecord, bool) {
	return harnessruntime.NewSnapshotStoreService(a.server.ensureSnapshotStore()).LoadRecord(runID)
}

func (a runtimeCoordinationAdapter) CancelRun(runID string) bool {
	return a.server.cancelActiveRun(runID)
}

func (a runtimeQueryAdapter) LoadRunRecord(runID string) (harnessruntime.RunRecord, bool) {
	return harnessruntime.NewSnapshotStoreService(a.server.ensureSnapshotStore()).LoadRecord(runID)
}

func (a runtimeQueryAdapter) ListRunRecords(threadID string) []harnessruntime.RunRecord {
	return harnessruntime.NewSnapshotStoreService(a.server.ensureSnapshotStore()).ListRecords(threadID)
}

func (a runtimeQueryAdapter) HasThread(threadID string) bool {
	if store := a.server.ensureThreadStateStore(); store != nil {
		return store.HasThread(threadID)
	}
	return false
}

func (a runtimeEventAdapter) NextRunEventIndex(runID string) int {
	store := a.server.ensureEventStore()
	if store == nil {
		return 1
	}
	return store.NextRunEventIndex(runID)
}

func (a runtimeEventAdapter) AppendRunEvent(runID string, event harnessruntime.RunEvent) {
	if store := a.server.ensureEventStore(); store != nil {
		store.AppendRunEvent(runID, event)
	}
	a.server.ensureRunRegistry().publish(runID, streamEventFromRuntimeEvent(event))
}

func (a runtimeEventAdapter) LoadRunEvents(runID string) []harnessruntime.RunEvent {
	store := a.server.ensureEventStore()
	if store == nil {
		return nil
	}
	return store.LoadRunEvents(runID)
}

func (a runtimeEventAdapter) SubscribeRunEvents(runID string, buffer int) (<-chan harnessruntime.RunEvent, func()) {
	source, unsubscribe := a.server.ensureRunRegistry().subscribe(runID, buffer)
	out := make(chan harnessruntime.RunEvent, buffer)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case event, ok := <-source:
				if !ok {
					return
				}
				select {
				case out <- runtimeEventFromStreamEvent(event):
				case <-done:
					return
				}
			}
		}
	}()
	return out, func() {
		once.Do(func() {
			close(done)
			unsubscribe()
		})
	}
}

func (a runtimeSnapshotAdapter) LoadRunSnapshot(runID string) (harnessruntime.RunSnapshot, bool) {
	store := a.server.ensureSnapshotStore()
	if store == nil {
		return harnessruntime.RunSnapshot{}, false
	}
	return store.LoadRunSnapshot(runID)
}

func (a runtimeSnapshotAdapter) ListRunSnapshots(threadID string) []harnessruntime.RunSnapshot {
	store := a.server.ensureSnapshotStore()
	if store == nil {
		return nil
	}
	return store.ListRunSnapshots(threadID)
}

func (a runtimeSnapshotAdapter) SaveRunSnapshot(snapshot harnessruntime.RunSnapshot) {
	if store := a.server.ensureSnapshotStore(); store != nil {
		store.SaveRunSnapshot(snapshot)
	}
}

func (a runtimeWorkerSpecAdapter) ResolveWorkerAgentSpec(threadID string, spec harnessruntime.PortableAgentSpec) harness.AgentSpec {
	resolved := spec.AgentSpec()
	if a.server == nil {
		return resolved
	}
	session := a.server.ensureSession(threadID, nil)
	resolved.PresentFiles = session.PresentFiles
	if len(a.server.mcpDeferredTools) > 0 {
		resolved.DeferredTools = append([]models.Tool(nil), a.server.mcpDeferredTools...)
	}
	return resolved
}
