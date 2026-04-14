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

func (fakeSessionRuntime) PlanMemoryScopes(threadID string, agentName string) harness.MemoryScopePlan {
	scope := pkgmemory.SessionScope(threadID)
	if agentName != "" {
		scope = pkgmemory.AgentScope(agentName)
	}
	return harness.NormalizeMemoryScopePlan(harness.MemoryScopePlan{
		Primary: scope,
		Inject:  []pkgmemory.Scope{scope},
		Update:  []pkgmemory.Scope{scope},
	})
}

type fakeTitleRuntime struct{}

func (fakeTitleRuntime) ComputeThreadTitle(_ context.Context, threadID string, modelName string, messages []models.Message) string {
	return threadID + ":" + modelName + ":" + messages[0].Content
}

type fakeCompletionRuntime struct {
	title       string
	taskState   harness.TaskState
	taskLife    TaskLifecycleDescriptor
	lifeCleared bool
	taskCleared bool
	interrupts  []any
	status      string
	cleared     bool
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
	taskState    harness.TaskState
	threadStatus string
	taskLife     TaskLifecycleDescriptor
	lifeCleared  bool
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

func (r *fakeCompletionRuntime) LoadThreadTaskState(_ string) harness.TaskState {
	return r.taskState
}

func (r *fakeCompletionRuntime) SetThreadTaskState(_ string, taskState harness.TaskState) {
	r.taskState = taskState
	r.taskCleared = false
}

func (r *fakeCompletionRuntime) ClearThreadTaskState(_ string) {
	r.taskState = harness.TaskState{}
	r.taskCleared = true
}

func (r *fakeCompletionRuntime) SetThreadTaskLifecycle(_ string, lifecycle TaskLifecycleDescriptor) {
	r.taskLife = lifecycle
	r.lifeCleared = false
}

func (r *fakeCompletionRuntime) ClearThreadTaskLifecycle(_ string) {
	r.taskLife = TaskLifecycleDescriptor{}
	r.lifeCleared = true
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

func (r *fakeRunStateRuntime) LoadThreadTaskState(_ string) harness.TaskState {
	return r.taskState
}

func (r *fakeRunStateRuntime) MarkThreadStatus(threadID string, status string) {
	r.threadStatus = threadID + ":" + status
}

func (r *fakeRunStateRuntime) SetThreadTaskLifecycle(_ string, lifecycle TaskLifecycleDescriptor) {
	r.taskLife = lifecycle
	r.lifeCleared = false
}

func (r *fakeRunStateRuntime) ClearThreadTaskLifecycle(_ string) {
	r.taskLife = TaskLifecycleDescriptor{}
	r.lifeCleared = true
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

func TestMemoryScopePlannerUsesThreadAndAgentNames(t *testing.T) {
	t.Parallel()

	planner := MemoryScopePlanner{
		Plan: func(threadID string, agentName string) harness.MemoryScopePlan {
			scope := pkgmemory.GroupScope(threadID + ":" + agentName)
			return harness.NormalizeMemoryScopePlan(harness.MemoryScopePlan{
				Primary: scope,
				Inject:  []pkgmemory.Scope{scope},
				Update:  []pkgmemory.Scope{scope},
			})
		},
	}

	plan := planner.PlanMemoryScopes(&harness.RunState{
		ThreadID:  "thread-1",
		AgentName: "lead_agent",
	})
	if plan.Primary.Type != pkgmemory.ScopeGroup || plan.Primary.ID != "thread-1:lead_agent" {
		t.Fatalf("plan = %+v", plan)
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
	plan := NewMemoryScopePlanner(fakeSessionRuntime{}).PlanMemoryScopes(&harness.RunState{
		ThreadID:  "thread-1",
		AgentName: "lead_agent",
	})
	if plan.Primary.Key() != "agent:lead_agent" || len(plan.Update) != 1 {
		t.Fatalf("plan = %+v", plan)
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

func TestCompletionServiceMarksRunIncompleteWhenPendingTasksRemain(t *testing.T) {
	t.Parallel()

	runtime := &fakeCompletionRuntime{}
	service := NewCompletionService(runtime, "generated_title", "clarification_interrupt")
	outcome := service.Apply("thread-1", &harness.RunState{
		ThreadID: "thread-1",
		Metadata: map[string]any{
			DefaultCompletionPendingTasksKey: []string{"write summary", "verify artifact"},
		},
	}, &agent.RunResult{})
	if outcome.Interrupted {
		t.Fatalf("Interrupted = true, want false")
	}
	if outcome.RunStatus != "incomplete" {
		t.Fatalf("RunStatus = %q, want %q", outcome.RunStatus, "incomplete")
	}
	if outcome.Descriptor.Error != "pending-tasks" {
		t.Fatalf("Descriptor.Error = %q, want %q", outcome.Descriptor.Error, "pending-tasks")
	}
	if got := len(outcome.Descriptor.PendingTasks); got != 2 {
		t.Fatalf("Descriptor.PendingTasks=%v want len=2", outcome.Descriptor.PendingTasks)
	}
	if runtime.status != "idle" || !runtime.cleared {
		t.Fatalf("unexpected runtime state: status=%q cleared=%v", runtime.status, runtime.cleared)
	}
}

func TestCompletionServiceMarksRunIncompleteFromTaskState(t *testing.T) {
	t.Parallel()

	runtime := &fakeCompletionRuntime{}
	service := NewCompletionService(runtime, "generated_title", "clarification_interrupt")
	outcome := service.Apply("thread-1", &harness.RunState{
		ThreadID: "thread-1",
		TaskState: harness.TaskState{
			Items: []harness.TaskItem{
				{Text: "write summary", Status: harness.TaskStatusInProgress},
				{Text: "verify artifact", Status: harness.TaskStatusPending},
			},
		},
	}, &agent.RunResult{})
	if outcome.RunStatus != "incomplete" {
		t.Fatalf("RunStatus = %q, want %q", outcome.RunStatus, "incomplete")
	}
	if outcome.Descriptor.Error != "pending-tasks" {
		t.Fatalf("Descriptor.Error = %q, want %q", outcome.Descriptor.Error, "pending-tasks")
	}
	if got := len(outcome.Descriptor.PendingTasks); got != 2 {
		t.Fatalf("Descriptor.PendingTasks=%v want len=2", outcome.Descriptor.PendingTasks)
	}
	if got := len(outcome.Descriptor.TaskState.Items); got != 2 {
		t.Fatalf("Descriptor.TaskState=%+v", outcome.Descriptor.TaskState)
	}
	if got := len(runtime.taskState.Items); got != 2 {
		t.Fatalf("taskState.Items=%d want=2", got)
	}
	if runtime.taskLife.Status != "incomplete" || len(runtime.taskLife.PendingTasks) != 2 {
		t.Fatalf("taskLife=%+v", runtime.taskLife)
	}
}

func TestCompletionServiceMergesLiveTrackedTaskState(t *testing.T) {
	t.Parallel()

	runtime := &fakeCompletionRuntime{
		taskState: harness.TaskState{
			Items: []harness.TaskItem{
				{Text: "delegate research", Status: harness.TaskStatusInProgress},
			},
		},
	}
	service := NewCompletionService(runtime, "generated_title", "clarification_interrupt")
	outcome := service.Apply("thread-1", &harness.RunState{
		ThreadID: "thread-1",
	}, &agent.RunResult{})
	if outcome.RunStatus != "incomplete" {
		t.Fatalf("RunStatus = %q, want incomplete", outcome.RunStatus)
	}
	if len(outcome.Descriptor.PendingTasks) != 1 || outcome.Descriptor.PendingTasks[0] != "delegate research" {
		t.Fatalf("Descriptor.PendingTasks=%v", outcome.Descriptor.PendingTasks)
	}
	if len(outcome.Descriptor.TaskState.Items) != 1 || outcome.Descriptor.TaskState.Items[0].Text != "delegate research" {
		t.Fatalf("Descriptor.TaskState=%+v", outcome.Descriptor.TaskState)
	}
	if runtime.taskLife.Status != "incomplete" {
		t.Fatalf("taskLife=%+v", runtime.taskLife)
	}
}

func TestCompletionServiceDerivesTaskStateFromWriteTodosResult(t *testing.T) {
	t.Parallel()

	runtime := &fakeCompletionRuntime{}
	service := NewCompletionService(runtime, "generated_title", "clarification_interrupt")
	outcome := service.Apply("thread-1", &harness.RunState{
		ThreadID: "thread-1",
	}, &agent.RunResult{
		Messages: []models.Message{{
			Role: models.RoleTool,
			ToolResult: &models.ToolResult{
				ToolName: "write_todos",
				Status:   models.CallStatusCompleted,
				Data: map[string]any{
					"todos": []any{
						map[string]any{"content": "draft summary", "status": "completed"},
						map[string]any{"content": "verify artifact", "status": "pending"},
					},
				},
			},
		}},
	})
	if outcome.RunStatus != "incomplete" {
		t.Fatalf("RunStatus = %q, want %q", outcome.RunStatus, "incomplete")
	}
	if outcome.Descriptor.Error != "pending-tasks" {
		t.Fatalf("Descriptor.Error = %q, want %q", outcome.Descriptor.Error, "pending-tasks")
	}
}

func TestCompletionServiceAllowsVerifiedExpectedOutputs(t *testing.T) {
	t.Parallel()

	runtime := &fakeCompletionRuntime{}
	service := NewCompletionService(runtime, "generated_title", "clarification_interrupt")
	outcome := service.Apply("thread-1", &harness.RunState{
		ThreadID: "thread-1",
		TaskState: harness.TaskState{
			Items: []harness.TaskItem{
				{Text: "draft report", Status: harness.TaskStatusCompleted},
				{Text: "present report", Status: harness.TaskStatusCompleted},
			},
			ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
		},
	}, &agent.RunResult{
		Messages: []models.Message{{
			Role: models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "call-present",
				ToolName: "present_files",
				Status:   models.CallStatusCompleted,
				Data: map[string]any{
					"filepaths": []any{"/mnt/user-data/outputs/report.md"},
				},
			},
		}},
	})
	if outcome.RunStatus != "success" {
		t.Fatalf("RunStatus = %q, want %q", outcome.RunStatus, "success")
	}
	if len(outcome.Descriptor.PendingTasks) != 0 || len(outcome.Descriptor.ExpectedArtifacts) != 0 {
		t.Fatalf("Descriptor=%+v want no pending task details", outcome.Descriptor)
	}
	if len(outcome.Descriptor.TaskState.VerifiedOutputs) != 1 || outcome.Descriptor.TaskState.VerifiedOutputs[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("Descriptor.TaskState=%+v", outcome.Descriptor.TaskState)
	}
	if got := len(runtime.taskState.VerifiedOutputs); got != 1 {
		t.Fatalf("VerifiedOutputs=%v want len=1", runtime.taskState.VerifiedOutputs)
	}
	if runtime.taskState.VerifiedOutputs[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("VerifiedOutputs=%v", runtime.taskState.VerifiedOutputs)
	}
	if runtime.taskLife.Status != "success" || len(runtime.taskLife.VerifiedArtifacts) != 1 {
		t.Fatalf("taskLife=%+v", runtime.taskLife)
	}
}

func TestCompletionServiceMarksClarificationInterruptAsPausedLifecycle(t *testing.T) {
	t.Parallel()

	runtime := &fakeCompletionRuntime{}
	service := NewCompletionService(runtime, "generated_title", "clarification_interrupt")
	outcome := service.Apply("thread-1", &harness.RunState{
		ThreadID: "thread-1",
		TaskState: harness.TaskState{
			Items: []harness.TaskItem{{Text: "clarify requirement", Status: harness.TaskStatusInProgress}},
		},
		Metadata: map[string]any{
			"clarification_interrupt": map[string]any{"question": "Need detail?"},
		},
	}, &agent.RunResult{})
	if outcome.RunStatus != "interrupted" || !outcome.Interrupted {
		t.Fatalf("outcome=%+v", outcome)
	}
	if outcome.Descriptor.TaskLifecycle.Status != "paused" {
		t.Fatalf("task lifecycle=%+v", outcome.Descriptor.TaskLifecycle)
	}
	if runtime.taskLife.Status != "paused" {
		t.Fatalf("runtime task lifecycle=%+v", runtime.taskLife)
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
	if desc.TaskLifecycle.Status != "interrupted" {
		t.Fatalf("descriptor lifecycle = %+v", desc.TaskLifecycle)
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
		TaskStateMetadataKey: DefaultTaskStateMetadataKey,
	}.BuildHooks(features, LifecycleProviders{
		MemoryRuntime:  &harness.MemoryRuntime{},
		Summarizer:     Summarizer{},
		MemoryPlanner:  MemoryScopePlanner{},
		TaskState:      TaskStateProvider{},
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
	if runtime.metadata["assistant_id"] != "lead_agent" || runtime.metadata[DefaultRunIDMetadataKey] != "generated-id" {
		t.Fatalf("metadata = %#v", runtime.metadata)
	}
	if runtime.metadata[DefaultActiveRunMetadataKey] != "generated-id" {
		t.Fatalf("active run metadata = %#v", runtime.metadata[DefaultActiveRunMetadataKey])
	}
	lifecycle, ok := runtime.metadata[DefaultTaskLifecycleMetadataKey].(map[string]any)
	if !ok || lifecycle["status"] != "running" {
		t.Fatalf("task lifecycle metadata = %#v", runtime.metadata[DefaultTaskLifecycleMetadataKey])
	}
}

func TestRunStateServiceTransitionsRecords(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 11, 11, 0, 0, 0, time.UTC)
	runtime := &fakeRunStateRuntime{
		taskState: harness.TaskState{
			Items:           []harness.TaskItem{{Text: "verify artifact", Status: harness.TaskStatusPending}},
			ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
		},
	}
	service := NewRunStateService(runtime)
	service.now = func() time.Time { return now }

	record := RunRecord{RunID: "run-1", ThreadID: "thread-1", Status: "running"}
	record = service.Begin(record)
	if record.Status != "running" || record.Outcome.RunStatus != "running" || runtime.threadStatus != "thread-1:busy" {
		t.Fatalf("begin record = %+v runtime=%+v", record, runtime)
	}
	if runtime.taskLife.Status != "running" {
		t.Fatalf("begin task lifecycle = %+v", runtime.taskLife)
	}
	if len(record.Outcome.TaskState.Items) != 1 || record.Outcome.TaskState.Items[0].Text != "verify artifact" {
		t.Fatalf("begin task state = %+v", record.Outcome.TaskState)
	}
	if len(record.Outcome.PendingTasks) != 1 || record.Outcome.PendingTasks[0] != "verify artifact" {
		t.Fatalf("begin pending tasks = %#v", record.Outcome.PendingTasks)
	}
	if len(record.Outcome.ExpectedArtifacts) != 1 || record.Outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("begin expected artifacts = %#v", record.Outcome.ExpectedArtifacts)
	}

	record = service.MarkError(record, context.DeadlineExceeded)
	if record.Status != "error" || record.Outcome.RunStatus != "error" || runtime.threadStatus != "thread-1:error" {
		t.Fatalf("error record = %+v runtime=%+v", record, runtime)
	}
	if runtime.taskLife.Status != "error" {
		t.Fatalf("error task lifecycle = %+v", runtime.taskLife)
	}
	if len(record.Outcome.TaskState.Items) != 1 || record.Outcome.TaskState.Items[0].Text != "verify artifact" {
		t.Fatalf("error task state = %+v", record.Outcome.TaskState)
	}
	if len(record.Outcome.PendingTasks) != 1 || record.Outcome.PendingTasks[0] != "verify artifact" {
		t.Fatalf("error pending tasks = %#v", record.Outcome.PendingTasks)
	}
	if len(record.Outcome.ExpectedArtifacts) != 1 || record.Outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("error expected artifacts = %#v", record.Outcome.ExpectedArtifacts)
	}

	record = service.MarkCanceled(record)
	if record.Status != "interrupted" || !record.Outcome.Interrupted || runtime.threadStatus != "thread-1:interrupted" {
		t.Fatalf("canceled record = %+v runtime=%+v", record, runtime)
	}
	if runtime.taskLife.Status != "interrupted" {
		t.Fatalf("canceled task lifecycle = %+v", runtime.taskLife)
	}
	if len(record.Outcome.TaskState.Items) != 1 || record.Outcome.TaskState.Items[0].Text != "verify artifact" {
		t.Fatalf("canceled task state = %+v", record.Outcome.TaskState)
	}
	if len(record.Outcome.PendingTasks) != 1 || record.Outcome.PendingTasks[0] != "verify artifact" {
		t.Fatalf("canceled pending tasks = %#v", record.Outcome.PendingTasks)
	}
	if len(record.Outcome.ExpectedArtifacts) != 1 || record.Outcome.ExpectedArtifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("canceled expected artifacts = %#v", record.Outcome.ExpectedArtifacts)
	}

	record = service.Finalize(record, CompletionOutcome{
		RunOutcome: RunOutcome{RunStatus: "success"},
		Descriptor: RunOutcomeDescriptor{
			RunStatus:     "success",
			TaskLifecycle: TaskLifecycleDescriptor{Status: "success"},
		},
	})
	if record.Status != "success" || record.Outcome.RunStatus != "success" || runtime.saved.Status != "success" {
		t.Fatalf("finalized record = %+v runtime=%+v", record, runtime)
	}
	if record.Outcome.Attempt != record.Attempt || record.Outcome.ResumeFromEvent != record.ResumeFromEvent || record.Outcome.ResumeReason != record.ResumeReason {
		t.Fatalf("finalized outcome recovery fields = %+v record=%+v", record.Outcome, record)
	}
	if runtime.taskLife.Status != "success" {
		t.Fatalf("finalized task lifecycle = %+v", runtime.taskLife)
	}
}

func TestRunStateServiceMaintainsActiveRunOwnership(t *testing.T) {
	t.Parallel()

	threads := NewInMemoryThreadStateStore()
	runtime := workerRunStateRuntime{
		snapshots: NewInMemoryRunStore(),
		threads:   threads,
	}
	service := NewRunStateService(runtime)

	runA := RunRecord{RunID: "run-a", ThreadID: "thread-1", Status: "running"}
	runB := RunRecord{RunID: "run-b", ThreadID: "thread-1", Status: "running"}
	service.Begin(runA)
	service.Begin(runB)

	state, ok := threads.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("LoadThreadRuntimeState() = false, want true")
	}
	if got := metadataRunID(state.Metadata[DefaultActiveRunMetadataKey]); got != "run-b" {
		t.Fatalf("active run id = %q, want %q", got, "run-b")
	}

	service.Finalize(runA, CompletionOutcome{
		RunOutcome: RunOutcome{RunStatus: "success"},
		Descriptor: RunOutcomeDescriptor{RunStatus: "success"},
	})
	state, ok = threads.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("LoadThreadRuntimeState() = false after finalize")
	}
	if got := metadataRunID(state.Metadata[DefaultActiveRunMetadataKey]); got != "run-b" {
		t.Fatalf("active run id after stale finalize = %q, want %q", got, "run-b")
	}

	service.MarkCanceled(runB)
	state, ok = threads.LoadThreadRuntimeState("thread-1")
	if !ok {
		t.Fatal("LoadThreadRuntimeState() = false after cancel")
	}
	if got := metadataRunID(state.Metadata[DefaultActiveRunMetadataKey]); got != "" {
		t.Fatalf("active run id after owner cancel = %q, want empty", got)
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
