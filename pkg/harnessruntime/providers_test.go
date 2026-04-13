package harnessruntime

import (
	"context"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type fakeConversationRuntime struct{}

func (fakeConversationRuntime) HistorySummary(threadID string) string {
	return "summary:" + threadID
}

func (fakeConversationRuntime) PersistHistorySummary(threadID string, summary string) {}

func (fakeConversationRuntime) CompactConversation(_ context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) harness.SummarizationCompaction {
	return harness.SummarizationCompaction{
		Summary:  existingSummary + ":" + modelName,
		Messages: messages,
		Changed:  true,
	}
}

type fakeSessionRuntime struct{}

func (fakeSessionRuntime) ResolveMemorySessionID(threadID string, agentName string) string {
	return threadID + "/" + agentName
}

func (fakeSessionRuntime) ResolveMemoryScope(threadID string, agentName string) pkgmemory.Scope {
	if agentName != "" {
		return pkgmemory.AgentScope(agentName)
	}
	return pkgmemory.SessionScope(threadID)
}

type fakeTitleRuntime struct{}

func (fakeTitleRuntime) ComputeThreadTitle(_ context.Context, threadID string, modelName string, messages []models.Message) string {
	return threadID + ":" + modelName + ":" + messages[0].Content
}

type fakeCompletionRuntime struct {
	title      string
	interrupts []any
	status     string
	cleared    bool
}

type fakePreflightRuntime struct {
	snapshot     SessionSnapshot
	saved        RunRecord
	metadata     map[string]any
	clearedKey   string
	threadStatus string
}

type fakeRunStateRuntime struct {
	saved        RunRecord
	threadStatus string
}

type fakeCoordinationRuntime struct {
	record   RunRecord
	found    bool
	canceled bool
}

type fakeContextRuntime struct{}

type fakeContextBinder struct {
	bound harness.ContextSpec
}

func (r *fakeCompletionRuntime) SetThreadTitle(_ string, title string) {
	r.title = title
}

func (r *fakeCompletionRuntime) SetThreadInterrupts(_ string, interrupts []any) {
	r.interrupts = append([]any(nil), interrupts...)
}

func (r *fakeCompletionRuntime) ClearThreadInterrupts(_ string) {
	r.cleared = true
	r.interrupts = nil
}

func (r *fakeCompletionRuntime) MarkThreadStatus(_ string, status string) {
	r.status = status
}

func (r *fakePreflightRuntime) PrepareSession(_ string) SessionSnapshot {
	return r.snapshot
}

func (r *fakePreflightRuntime) MarkThreadStatus(_ string, status string) {
	r.threadStatus = status
}

func (r *fakePreflightRuntime) SetThreadMetadata(_ string, key string, value any) {
	if r.metadata == nil {
		r.metadata = map[string]any{}
	}
	r.metadata[key] = value
}

func (r *fakePreflightRuntime) ClearThreadMetadata(_ string, key string) {
	r.clearedKey = key
}

func (r *fakePreflightRuntime) SaveRunRecord(record RunRecord) {
	r.saved = record
}

func (r *fakeRunStateRuntime) SaveRunRecord(record RunRecord) {
	r.saved = record
}

func (r *fakeRunStateRuntime) MarkThreadStatus(threadID string, status string) {
	r.threadStatus = threadID + ":" + status
}

func (r *fakeCoordinationRuntime) LoadRunRecord(_ string) (RunRecord, bool) {
	return r.record, r.found
}

func (r *fakeCoordinationRuntime) CancelRun(_ string) bool {
	r.canceled = true
	return true
}

func (fakeContextRuntime) ClarificationManager() *clarification.Manager {
	return clarification.NewManager(4)
}

func (fakeContextRuntime) SkillPaths() any {
	return []string{"/mnt/skills/public/frontend-design/SKILL.md"}
}

func (b *fakeContextBinder) BindContext(ctx context.Context, spec harness.ContextSpec) context.Context {
	b.bound = spec
	return ctx
}

func TestSummarizerCompactUsesConfiguredFunctions(t *testing.T) {
	t.Parallel()

	var loadedThread string
	var persistedThread string
	var persistedSummary string

	summarizer := Summarizer{
		LoadSummary: func(threadID string) string {
			loadedThread = threadID
			return "existing summary"
		},
		CompactConversation: func(_ context.Context, threadID string, modelName string, existingSummary string, messages []models.Message) harness.SummarizationCompaction {
			if threadID != "thread-1" {
				t.Fatalf("unexpected threadID: %s", threadID)
			}
			if modelName != "model-1" {
				t.Fatalf("unexpected modelName: %s", modelName)
			}
			if existingSummary != "existing summary" {
				t.Fatalf("unexpected existingSummary: %s", existingSummary)
			}
			if len(messages) != 2 {
				t.Fatalf("unexpected messages len: %d", len(messages))
			}
			return harness.SummarizationCompaction{
				Summary:  "updated summary",
				Messages: messages[1:],
				Changed:  true,
			}
		},
		Persist: func(threadID string, summary string) {
			persistedThread = threadID
			persistedSummary = summary
		},
	}

	compacted, err := summarizer.Compact(context.Background(), &harness.RunState{
		ThreadID: "thread-1",
		Model:    "model-1",
		Messages: []models.Message{
			{Role: models.RoleHuman, Content: "hello"},
			{Role: models.RoleAI, Content: "world"},
		},
	})
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if loadedThread != "thread-1" {
		t.Fatalf("LoadSummary thread = %q, want %q", loadedThread, "thread-1")
	}
	if compacted.Summary != "updated summary" || !compacted.Changed || len(compacted.Messages) != 1 {
		t.Fatalf("unexpected compaction result: %+v", compacted)
	}

	summarizer.PersistSummary("thread-1", "saved")
	if persistedThread != "thread-1" || persistedSummary != "saved" {
		t.Fatalf("persist mismatch: thread=%q summary=%q", persistedThread, persistedSummary)
	}
}

func TestMemorySessionResolverUsesThreadAndAgentNames(t *testing.T) {
	t.Parallel()

	resolver := MemorySessionResolver{
		Resolve: func(threadID string, agentName string) string {
			return threadID + ":" + agentName
		},
	}

	sessionID := resolver.ResolveMemorySession(&harness.RunState{
		ThreadID:  "thread-1",
		AgentName: "lead_agent",
	})
	if sessionID != "thread-1:lead_agent" {
		t.Fatalf("sessionID = %q, want %q", sessionID, "thread-1:lead_agent")
	}
}

func TestMemoryScopeResolverUsesThreadAndAgentNames(t *testing.T) {
	t.Parallel()

	resolver := MemoryScopeResolver{
		Resolve: func(threadID string, agentName string) pkgmemory.Scope {
			return pkgmemory.GroupScope(threadID + ":" + agentName)
		},
	}

	scope := resolver.ResolveMemoryScope(&harness.RunState{
		ThreadID:  "thread-1",
		AgentName: "lead_agent",
	})
	if scope.Type != pkgmemory.ScopeGroup || scope.ID != "thread-1:lead_agent" {
		t.Fatalf("scope = %+v", scope)
	}
}

func TestTitleGeneratorUsesRunResultMessages(t *testing.T) {
	t.Parallel()

	generator := TitleGenerator{
		Generate: func(_ context.Context, threadID string, modelName string, messages []models.Message) string {
			if threadID != "thread-1" || modelName != "model-1" {
				t.Fatalf("unexpected state: %s %s", threadID, modelName)
			}
			if len(messages) != 1 || messages[0].Content != "assistant reply" {
				t.Fatalf("unexpected messages: %+v", messages)
			}
			return "generated title"
		},
	}

	title := generator.GenerateTitle(context.Background(), &harness.RunState{
		ThreadID: "thread-1",
		Model:    "model-1",
	}, &agent.RunResult{
		Messages: []models.Message{{Role: models.RoleAI, Content: "assistant reply"}},
	})
	if title != "generated title" {
		t.Fatalf("title = %q, want %q", title, "generated title")
	}
}

func TestConstructorsWrapRuntimeInterfaces(t *testing.T) {
	t.Parallel()

	summarizer := NewSummarizer(fakeConversationRuntime{})
	compacted, err := summarizer.Compact(context.Background(), &harness.RunState{
		ThreadID: "thread-1",
		Model:    "model-1",
		Messages: []models.Message{{Role: models.RoleHuman, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}
	if compacted.Summary != "summary:thread-1:model-1" {
		t.Fatalf("summary = %q", compacted.Summary)
	}

	resolver := NewMemorySessionResolver(fakeSessionRuntime{})
	sessionID := resolver.ResolveMemorySession(&harness.RunState{
		ThreadID:  "thread-1",
		AgentName: "lead_agent",
	})
	if sessionID != "thread-1/lead_agent" {
		t.Fatalf("sessionID = %q", sessionID)
	}
	scope := NewMemoryScopeResolver(fakeSessionRuntime{}).ResolveMemoryScope(&harness.RunState{
		ThreadID:  "thread-1",
		AgentName: "lead_agent",
	})
	if scope.Key() != "agent:lead_agent" {
		t.Fatalf("scope key = %q", scope.Key())
	}

	title := NewTitleGenerator(fakeTitleRuntime{}).GenerateTitle(context.Background(), &harness.RunState{
		ThreadID: "thread-1",
		Model:    "model-1",
	}, &agent.RunResult{
		Messages: []models.Message{{Role: models.RoleAI, Content: "reply"}},
	})
	if title != "thread-1:model-1:reply" {
		t.Fatalf("title = %q", title)
	}
}

func TestMemoryServiceBuildsHarnessRuntime(t *testing.T) {
	t.Parallel()

	store, err := pkgmemory.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	service := NewMemoryService(store, nil)
	if !service.Enabled() {
		t.Fatalf("Enabled() = false, want true")
	}
	if service.Runtime() == nil {
		t.Fatalf("Runtime() = nil")
	}
	if service.Store() == nil {
		t.Fatalf("Store() = nil")
	}
	if service.Runtime().Updater() == nil {
		t.Fatalf("Updater() = nil")
	}
}

func TestCompletionServiceAppliesTitleAndInterruptState(t *testing.T) {
	t.Parallel()

	runtime := &fakeCompletionRuntime{}
	service := NewCompletionService(runtime, "generated_title", "clarification_interrupt")

	outcome := service.Apply("thread-1", &harness.RunState{
		ThreadID: "thread-1",
		Metadata: map[string]any{
			"generated_title": "Title 1",
			"clarification_interrupt": map[string]any{
				"value": "Need input",
			},
		},
	}, &agent.RunResult{})
	if !outcome.Interrupted {
		t.Fatalf("Interrupted = false, want true")
	}
	if runtime.title != "Title 1" {
		t.Fatalf("title = %q", runtime.title)
	}
	if runtime.status != "interrupted" || len(runtime.interrupts) != 1 {
		t.Fatalf("unexpected runtime state: status=%q interrupts=%#v", runtime.status, runtime.interrupts)
	}
}

func TestCompletionServiceFallsBackToIdleWithoutInterrupt(t *testing.T) {
	t.Parallel()

	runtime := &fakeCompletionRuntime{}
	service := NewCompletionService(runtime, "generated_title", "clarification_interrupt")
	outcome := service.Apply("thread-1", &harness.RunState{
		ThreadID: "thread-1",
	}, &agent.RunResult{})
	if outcome.Interrupted {
		t.Fatalf("Interrupted = true, want false")
	}
	if outcome.RunStatus != "success" {
		t.Fatalf("RunStatus = %q, want %q", outcome.RunStatus, "success")
	}
	if runtime.status != "idle" || !runtime.cleared {
		t.Fatalf("unexpected runtime state: status=%q cleared=%v", runtime.status, runtime.cleared)
	}
}

func TestOutcomeServiceMapsInterruptedState(t *testing.T) {
	t.Parallel()

	outcomes := NewOutcomeService()
	if got := outcomes.Resolve(true); !got.Interrupted || got.RunStatus != "interrupted" {
		t.Fatalf("interrupted outcome = %+v", got)
	}
	if got := outcomes.Resolve(false); got.Interrupted || got.RunStatus != "success" {
		t.Fatalf("success outcome = %+v", got)
	}
	desc := outcomes.Describe(RunRecord{
		Attempt:         2,
		ResumeFromEvent: 5,
		ResumeReason:    "retry",
	}, outcomes.Resolve(true), "boom")
	if desc.RunStatus != "interrupted" || !desc.Interrupted || desc.Error != "boom" {
		t.Fatalf("descriptor = %+v", desc)
	}
	if desc.Attempt != 2 || desc.ResumeFromEvent != 5 || desc.ResumeReason != "retry" {
		t.Fatalf("descriptor recovery = %+v", desc)
	}
}

func TestFeatureConfigBuildAssembly(t *testing.T) {
	t.Parallel()

	manager := clarification.NewManager(8)
	assembly := FeatureConfig{
		ClarificationEnabled: true,
		MemoryEnabled:        true,
		SummarizationEnabled: true,
		TitleEnabled:         false,
	}.BuildAssembly(manager)
	if !assembly.Clarification.Enabled || assembly.Clarification.Manager != manager {
		t.Fatalf("clarification assembly = %+v", assembly.Clarification)
	}
	if !assembly.Memory.Enabled || !assembly.Summarization.Enabled || assembly.Title.Enabled {
		t.Fatalf("assembly = %+v", assembly)
	}
}

func TestLifecycleConfigBuildHooks(t *testing.T) {
	t.Parallel()

	features := harness.FeatureAssembly{
		Summarization: harness.SummarizationFeature{Enabled: true},
		Memory:        harness.MemoryFeature{Enabled: true},
		Clarification: harness.ClarificationFeature{Enabled: true},
		Title:         harness.TitleFeature{Enabled: true},
	}
	hooks := LifecycleConfig{
		SummaryMetadataKey:   "history_summary",
		MemorySessionKey:     "memory_session_id",
		InterruptMetadataKey: "clarification_interrupt",
		TitleMetadataKey:     "generated_title",
	}.BuildHooks(features, LifecycleProviders{
		MemoryRuntime:  &harness.MemoryRuntime{},
		Summarizer:     Summarizer{},
		MemoryResolver: MemoryScopeResolver{},
		TitleGenerator: TitleGenerator{},
	})
	if hooks == nil {
		t.Fatal("BuildHooks() = nil")
	}
}

func TestOrchestratorPrepareBuildsLifecycleAndExecution(t *testing.T) {
	t.Parallel()

	orchestrator := NewOrchestrator(nil, nil)
	prepared, err := orchestrator.Prepare(context.Background(), RunPlan{
		ThreadID:        "thread-1",
		AssistantID:     "lead_agent",
		RunID:           "run-1",
		SubmittedAt:     time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
		Attempt:         2,
		ResumeFromEvent: 5,
		ResumeReason:    "replay",
		Model:           "model-1",
		AgentName:       "lead_agent",
		Spec:            harness.AgentSpec{},
		Features:        harness.FeatureSet{Sandbox: true},
		Messages:        []models.Message{{Role: models.RoleHuman, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared == nil || prepared.Lifecycle == nil || prepared.Execution == nil {
		t.Fatalf("prepared = %#v", prepared)
	}
	if prepared.Lifecycle.ThreadID != "thread-1" || prepared.Lifecycle.AssistantID != "lead_agent" {
		t.Fatalf("lifecycle = %+v", prepared.Lifecycle)
	}
	if got := prepared.Lifecycle.Metadata["run_id"]; got != "run-1" {
		t.Fatalf("run_id metadata = %#v", got)
	}
	if got := prepared.Lifecycle.Metadata["submission_attempt"]; got != 2 {
		t.Fatalf("submission_attempt metadata = %#v", got)
	}
	if got := prepared.Lifecycle.Metadata["resume_from_event_index"]; got != 5 {
		t.Fatalf("resume_from_event_index metadata = %#v", got)
	}
	if got := prepared.Lifecycle.Metadata["resume_reason"]; got != "replay" {
		t.Fatalf("resume_reason metadata = %#v", got)
	}
}

func TestPreflightServicePreparesRunState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	runtime := &fakePreflightRuntime{
		snapshot: SessionSnapshot{
			ExistingMessages: []models.Message{{Role: models.RoleHuman, Content: "old"}},
		},
	}
	service := NewPreflightService(runtime)
	service.now = func() time.Time { return now }
	service.newID = func() string { return "generated-id" }

	result := service.Prepare(PreflightInput{
		RequestedThreadID: "thread-1",
		AssistantID:       "lead_agent",
		NewMessages:       []models.Message{{Role: models.RoleHuman, Content: "new"}},
	})

	if result.ThreadID != "thread-1" || result.AssistantID != "lead_agent" {
		t.Fatalf("result ids = %+v", result)
	}
	if result.Run.RunID != "generated-id" || result.Run.Status != "running" {
		t.Fatalf("run = %+v", result.Run)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("messages len = %d", len(result.Messages))
	}
	if runtime.threadStatus != "busy" || runtime.clearedKey != "interrupts" {
		t.Fatalf("runtime state = %+v", runtime)
	}
	if runtime.metadata["assistant_id"] != "lead_agent" || runtime.metadata["run_id"] != "generated-id" {
		t.Fatalf("metadata = %#v", runtime.metadata)
	}
}

func TestRunStateServiceTransitionsRecords(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 11, 0, 0, 0, time.UTC)
	runtime := &fakeRunStateRuntime{}
	service := NewRunStateService(runtime)
	service.now = func() time.Time { return now }

	record := RunRecord{RunID: "run-1", ThreadID: "thread-1", Status: "running"}
	record = service.Begin(record)
	if record.Status != "running" || record.Outcome.RunStatus != "running" || runtime.threadStatus != "thread-1:busy" {
		t.Fatalf("begin record = %+v runtime=%+v", record, runtime)
	}

	record = service.MarkError(record, context.DeadlineExceeded)
	if record.Status != "error" || record.Outcome.RunStatus != "error" || runtime.threadStatus != "thread-1:error" {
		t.Fatalf("error record = %+v runtime=%+v", record, runtime)
	}

	record = service.MarkCanceled(record)
	if record.Status != "interrupted" || !record.Outcome.Interrupted || runtime.threadStatus != "thread-1:interrupted" {
		t.Fatalf("canceled record = %+v runtime=%+v", record, runtime)
	}

	record = service.Finalize(record, CompletionOutcome{
		RunOutcome: RunOutcome{RunStatus: "success"},
		Descriptor: RunOutcomeDescriptor{RunStatus: "success"},
	})
	if record.Status != "success" || record.Outcome.RunStatus != "success" || runtime.saved.Status != "success" {
		t.Fatalf("finalized record = %+v runtime=%+v", record, runtime)
	}
}

func TestContextServiceBuildsAndBindsSpec(t *testing.T) {
	t.Parallel()

	binder := &fakeContextBinder{}
	service := NewContextService(fakeContextRuntime{}, binder)
	ctx := service.Bind(context.Background(), "thread-1", harness.RunHooks{})
	if ctx == nil {
		t.Fatal("Bind() returned nil context")
	}
	if binder.bound.ThreadID != "thread-1" {
		t.Fatalf("thread id = %q", binder.bound.ThreadID)
	}
	paths, _ := binder.bound.RuntimeContext["skill_paths"].([]string)
	if len(paths) != 1 || paths[0] == "" {
		t.Fatalf("skill_paths = %#v", binder.bound.RuntimeContext["skill_paths"])
	}
	if binder.bound.ClarificationManager == nil {
		t.Fatal("ClarificationManager is nil")
	}
}

func TestCoordinationServiceWaitAndCancel(t *testing.T) {
	t.Parallel()

	runtime := &fakeCoordinationRuntime{
		record: RunRecord{RunID: "run-1", ThreadID: "thread-1", Status: "success"},
		found:  true,
	}
	service := NewCoordinationService(runtime)
	record, found, completed := service.Wait(context.Background(), "thread-1", "run-1", false)
	if !found || !completed || record.RunID != "run-1" {
		t.Fatalf("wait result = %+v found=%v completed=%v", record, found, completed)
	}

	runtime.record.Status = "running"
	resp, found, canceled := service.Cancel("thread-1", "run-1")
	if !found || !canceled || resp["status"] != "interrupted" || !runtime.canceled {
		t.Fatalf("cancel result = %#v found=%v canceled=%v runtime=%+v", resp, found, canceled, runtime)
	}
}
