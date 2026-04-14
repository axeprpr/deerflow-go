package langgraphcompat

import (
	"context"
	"strings"
	"sync"

	"github.com/axeprpr/deerflow-go/pkg/agent"
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

func (a runtimeConversationAdapter) LoadTaskState(threadID string) harness.TaskState {
	if a.server == nil {
		return harness.TaskState{}
	}
	session := a.server.ensureSession(threadID, nil)
	base := harness.TaskState{}
	if store := a.server.ensureThreadStateStore(); store != nil {
		if state, ok := store.LoadThreadRuntimeState(threadID); ok {
			if taskState, ok := harness.ParseTaskState(state.Metadata[harnessruntime.DefaultTaskStateMetadataKey]); ok {
				base = taskState
			}
		}
	}
	if state, ok := harness.ParseTaskState(session.Metadata[harnessruntime.DefaultTaskStateMetadataKey]); ok {
		if base.IsZero() {
			base = state
		} else if merged, err := harnessruntime.MergeTaskProgressState(base, state); err == nil {
			base = merged
		}
	} else if state, ok := taskStateFromTodosRaw(session.Metadata["todos"]); ok {
		if base.IsZero() {
			base = state
		} else if merged, err := harnessruntime.MergeTaskProgressState(base, state); err == nil {
			base = merged
		}
	}
	if len(session.Todos) > 0 {
		items := make([]harness.TaskItem, 0, len(session.Todos))
		for _, todo := range session.Todos {
			items = append(items, harness.TaskItem{
				Text:   strings.TrimSpace(todo.Content),
				Status: strings.TrimSpace(todo.Status),
			})
		}
		state, err := harness.NormalizeTaskState(harness.TaskState{Items: items})
		if err == nil {
			if base.IsZero() {
				return state
			}
			if merged, err := harnessruntime.MergeTaskProgressState(base, state); err == nil {
				return merged
			}
			return state
		}
	}
	return base
}

func (a runtimeConversationAdapter) DeriveTaskState(threadID string, result *agent.RunResult) harness.TaskState {
	loaded := a.LoadTaskState(threadID)
	if state, ok := deriveRuntimeTaskState(result); ok {
		if loaded.IsZero() {
			return state
		}
		if merged, err := harnessruntime.MergeTaskProgressState(loaded, state); err == nil {
			return merged
		}
		return state
	}
	return loaded
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
			scope := threadMemoryScopeFromMetadata(state.Metadata)
			hints.UserID = scope.UserID
			hints.GroupID = scope.GroupID
			hints.Namespace = scope.Namespace
		}
	}
	return harnessruntime.NewMemoryScopeService(nil).Plan(hints)
}

func (a runtimeCompletionAdapter) SetThreadTitle(threadID string, title string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.SetThreadMetadata(threadID, "title", title)
	}
}

func (a runtimeCompletionAdapter) LoadThreadTaskState(threadID string) harness.TaskState {
	if store := a.server.ensureThreadStateStore(); store != nil {
		if state, ok := store.LoadThreadRuntimeState(threadID); ok {
			if taskState, ok := harness.ParseTaskState(state.Metadata[harnessruntime.DefaultTaskStateMetadataKey]); ok {
				return taskState
			}
		}
	}
	return harness.TaskState{}
}

func (a runtimeCompletionAdapter) SetThreadTaskState(threadID string, taskState harness.TaskState) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.SetThreadMetadata(threadID, harnessruntime.DefaultTaskStateMetadataKey, taskState.Value())
	}
}

func (a runtimeCompletionAdapter) ClearThreadTaskState(threadID string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.ClearThreadMetadata(threadID, harnessruntime.DefaultTaskStateMetadataKey)
	}
}

func (a runtimeCompletionAdapter) SetThreadTaskLifecycle(threadID string, lifecycle harnessruntime.TaskLifecycleDescriptor) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.SetThreadMetadata(threadID, harnessruntime.DefaultTaskLifecycleMetadataKey, lifecycle.Value())
	}
}

func (a runtimeCompletionAdapter) ClearThreadTaskLifecycle(threadID string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.ClearThreadMetadata(threadID, harnessruntime.DefaultTaskLifecycleMetadataKey)
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

func (a runtimeRunStateAdapter) SetThreadTaskLifecycle(threadID string, lifecycle harnessruntime.TaskLifecycleDescriptor) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.SetThreadMetadata(threadID, harnessruntime.DefaultTaskLifecycleMetadataKey, lifecycle.Value())
	}
}

func (a runtimeRunStateAdapter) ClearThreadTaskLifecycle(threadID string) {
	if store := a.server.ensureThreadStateStore(); store != nil {
		store.ClearThreadMetadata(threadID, harnessruntime.DefaultTaskLifecycleMetadataKey)
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
	registrySource, registryUnsubscribe := a.server.ensureRunRegistry().subscribe(runID, buffer)
	var storeSource <-chan harnessruntime.RunEvent
	storeUnsubscribe := func() {}
	if store, ok := a.server.ensureEventStore().(harnessruntime.RunEventFeed); ok {
		storeSource, storeUnsubscribe = store.SubscribeRunEvents(runID, buffer)
	}
	out := make(chan harnessruntime.RunEvent, buffer)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(out)
		seen := map[string]struct{}{}
		forward := func(event harnessruntime.RunEvent) bool {
			if event.ID != "" {
				if _, ok := seen[event.ID]; ok {
					return true
				}
				seen[event.ID] = struct{}{}
			}
			select {
			case out <- event:
				return true
			case <-done:
				return false
			}
		}
		for {
			select {
			case <-done:
				return
			case event, ok := <-registrySource:
				if !ok {
					registrySource = nil
					if storeSource == nil {
						return
					}
					continue
				}
				if !forward(runtimeEventFromStreamEvent(event)) {
					return
				}
			case event, ok := <-storeSource:
				if !ok {
					storeSource = nil
					if registrySource == nil {
						return
					}
					continue
				}
				if !forward(event) {
					return
				}
			}
		}
	}()
	return out, func() {
		once.Do(func() {
			close(done)
			registryUnsubscribe()
			storeUnsubscribe()
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

func (a runtimeWorkerSpecAdapter) ResolveWorkerContextSpec(threadID string) harness.ContextSpec {
	spec := harness.ContextSpec{ThreadID: threadID}
	if a.server == nil {
		return spec
	}
	spec.ClarificationManager = a.server.clarify
	spec.RuntimeContext = map[string]any{
		"skill_paths": a.server.runtimeSkillPaths(),
	}
	return spec
}
