package langgraphcompat

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestRuntimeConversationAdapterSummaryAccessors(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	server.ensureSession("thread-1", nil)
	adapter := server.runtimeConversationAdapter()

	adapter.PersistHistorySummary("thread-1", "summary-1")
	if got := adapter.HistorySummary("thread-1"); got != "summary-1" {
		t.Fatalf("HistorySummary() = %q, want %q", got, "summary-1")
	}
}

func TestRuntimeConversationAdapterLoadTaskStatePreservesExpectedOutputs(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	session := server.ensureSession("thread-1", nil)
	session.Todos = []Todo{
		{Content: "draft report", Status: "completed"},
		{Content: "present report", Status: "in_progress"},
	}
	session.Metadata[harnessruntime.DefaultTaskStateMetadataKey] = harness.TaskState{
		Items: []harness.TaskItem{
			{Text: "draft report", Status: harness.TaskStatusCompleted},
			{Text: "present report", Status: harness.TaskStatusInProgress},
		},
		ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
	}.Value()

	state := server.runtimeConversationAdapter().LoadTaskState("thread-1")
	if got := len(state.ExpectedOutputs); got != 1 {
		t.Fatalf("ExpectedOutputs=%v want len=1", state.ExpectedOutputs)
	}
	if state.ExpectedOutputs[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("ExpectedOutputs=%v", state.ExpectedOutputs)
	}
}

func TestRuntimeConversationAdapterLoadTaskStateMergesThreadRuntimeState(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	session := server.ensureSession("thread-1", nil)
	session.Todos = []Todo{
		{Content: "draft report", Status: "completed"},
	}
	server.ensureThreadStateStore().SetThreadMetadata("thread-1", harnessruntime.DefaultTaskStateMetadataKey, harness.TaskState{
		Items: []harness.TaskItem{
			{Text: "present report", Status: harness.TaskStatusInProgress},
		},
		ExpectedOutputs: []string{"/mnt/user-data/outputs/report.md"},
		VerifiedOutputs: []string{"/mnt/user-data/outputs/report.md"},
	}.Value())

	state := server.runtimeConversationAdapter().LoadTaskState("thread-1")
	if len(state.Items) != 2 {
		t.Fatalf("Items=%+v", state.Items)
	}
	if got := len(state.ExpectedOutputs); got != 1 || state.ExpectedOutputs[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("ExpectedOutputs=%v", state.ExpectedOutputs)
	}
	if got := len(state.VerifiedOutputs); got != 1 || state.VerifiedOutputs[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("VerifiedOutputs=%v", state.VerifiedOutputs)
	}
}

func TestRuntimeMemoryAdapterResolveMemorySessionID(t *testing.T) {
	adapter := (&Server{}).runtimeMemoryAdapter()
	if got := adapter.ResolveMemorySessionID("thread-1", "planner"); got != "agent:planner" {
		t.Fatalf("ResolveMemorySessionID() = %q, want %q", got, "agent:planner")
	}
	scope := adapter.ResolveMemoryScope("thread-1", "planner")
	if scope.Key() != "agent:planner" {
		t.Fatalf("ResolveMemoryScope().Key() = %q, want %q", scope.Key(), "agent:planner")
	}
}

func TestRuntimeMemoryAdapterPlansSharedScopesFromThreadMetadata(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	store := server.ensureThreadStateStore()
	store.SetThreadMetadata("thread-1", "memory_user_id", "user-1")
	store.SetThreadMetadata("thread-1", "memory_group_id", "group-1")
	store.SetThreadMetadata("thread-1", "memory_namespace", "project-a")

	plan := server.runtimeMemoryAdapter().PlanMemoryScopes("thread-1", "planner")
	wantPrimary := "__scope__:agent:planner:project-a"
	if plan.Primary.Key() != wantPrimary {
		t.Fatalf("Primary = %q, want %q", plan.Primary.Key(), wantPrimary)
	}
	if len(plan.Inject) != 4 || len(plan.Update) != 4 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestRuntimeConversationAdapterCompactConversationUsesExistingImplementation(t *testing.T) {
	server := &Server{
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	server.ensureSession("thread-1", nil)
	adapter := server.runtimeConversationAdapter()

	messages := make([]models.Message, 0, 34)
	for i := 0; i < 34; i++ {
		role := models.RoleHuman
		if i%2 == 1 {
			role = models.RoleAI
		}
		messages = append(messages, models.Message{
			ID:        "m",
			SessionID: "thread-1",
			Role:      role,
			Content:   "message content message content message content",
		})
	}

	compacted := adapter.CompactConversation(context.Background(), "thread-1", "model-1", "", messages)
	if !compacted.Changed {
		t.Fatalf("CompactConversation() did not mark result as changed")
	}
	if len(compacted.Messages) != defaultSummaryKeepMessages {
		t.Fatalf("kept messages = %d, want %d", len(compacted.Messages), defaultSummaryKeepMessages)
	}
}

func TestRuntimeWorkerSpecAdapterRehydratesThreadScopedState(t *testing.T) {
	server := &Server{
		sessions:         map[string]*Session{},
		runs:             map[string]*Run{},
		mcpDeferredTools: []models.Tool{{Name: "web_search"}},
	}
	session := server.ensureSession("thread-1", nil)
	session.PresentFiles = tools.NewPresentFileRegistry()

	got := server.runtimeWorkerSpecAdapter().ResolveWorkerAgentSpec("thread-1", harnessruntime.PortableAgentSpec{
		Model:        "model-1",
		SystemPrompt: "system",
	})

	if got.PresentFiles != session.PresentFiles {
		t.Fatalf("PresentFiles not rehydrated from session")
	}
	if len(got.DeferredTools) != 1 || got.DeferredTools[0].Name != "web_search" {
		t.Fatalf("DeferredTools = %#v", got.DeferredTools)
	}
	if got.Model != "model-1" || got.SystemPrompt != "system" {
		t.Fatalf("resolved spec = %#v", got)
	}
}

func TestRuntimeWorkerSpecAdapterResolvesWorkerContextSpec(t *testing.T) {
	server := &Server{
		clarify: clarification.NewManager(4),
	}

	spec := server.runtimeWorkerSpecAdapter().ResolveWorkerContextSpec("thread-1")
	if spec.ThreadID != "thread-1" {
		t.Fatalf("ThreadID = %q", spec.ThreadID)
	}
	if spec.ClarificationManager != server.clarify {
		t.Fatal("ClarificationManager not rehydrated")
	}
	if _, ok := spec.RuntimeContext["skill_paths"]; !ok {
		t.Fatalf("RuntimeContext = %#v", spec.RuntimeContext)
	}
}
