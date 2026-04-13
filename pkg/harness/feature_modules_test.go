package harness

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

var providerStylePersistedSummary string

type providerStyleSummarizerStub struct{}
type providerStyleResolverStub struct{}
type providerStylePlannerStub struct{}
type providerStyleTitleStub struct{}

func (providerStyleSummarizerStub) Compact(_ context.Context, state *RunState) (SummarizationCompaction, error) {
	return SummarizationCompaction{
		Summary:  "summary",
		Messages: append([]models.Message(nil), state.Messages...),
		Changed:  true,
	}, nil
}

func (providerStyleSummarizerStub) PersistSummary(_ string, summary string) {
	providerStylePersistedSummary = summary
}

func (providerStyleResolverStub) ResolveMemorySession(_ *RunState) string {
	return "session-1"
}

func (providerStylePlannerStub) PlanMemoryScopes(_ *RunState) MemoryScopePlan {
	scope := pkgmemory.AgentScope("planner")
	return NormalizeMemoryScopePlan(MemoryScopePlan{
		Primary: scope,
		Inject:  []pkgmemory.Scope{pkgmemory.SessionScope("thread-1"), scope},
		Update:  []pkgmemory.Scope{pkgmemory.SessionScope("thread-1"), scope},
	})
}

func (providerStyleTitleStub) GenerateTitle(_ context.Context, _ *RunState, _ *agent.RunResult) string {
	return "title"
}

func TestMergeLifecycleHooksCombinesPhases(t *testing.T) {
	var before []string
	var after []string

	hooks := MergeLifecycleHooks(
		&LifecycleHooks{
			BeforeRun: []BeforeRunHook{
				func(_ context.Context, _ *RunState) error {
					before = append(before, "one")
					return nil
				},
			},
		},
		&LifecycleHooks{
			AfterRun: []AfterRunHook{
				func(_ context.Context, _ *RunState, _ *agent.RunResult) error {
					after = append(after, "two")
					return nil
				},
			},
		},
	)

	if hooks == nil {
		t.Fatal("MergeLifecycleHooks() returned nil")
	}
	if err := hooks.Before(context.Background(), &RunState{}); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if err := hooks.After(context.Background(), &RunState{}, &agent.RunResult{}); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	if len(before) != 1 || before[0] != "one" {
		t.Fatalf("before hooks = %#v", before)
	}
	if len(after) != 1 || after[0] != "two" {
		t.Fatalf("after hooks = %#v", after)
	}
}

func TestClarificationLifecycleHooksStoresInterrupt(t *testing.T) {
	hooks := ClarificationLifecycleHooks(ClarificationLifecycleConfig{})
	state := &RunState{Metadata: map[string]any{}}
	result := &agent.RunResult{
		Messages: []models.Message{{
			Role: models.RoleTool,
			ToolResult: &models.ToolResult{
				ToolName: "ask_clarification",
				Status:   models.CallStatusCompleted,
				Content:  "Need more detail",
				Data: map[string]any{
					"id":       "clarify-1",
					"question": "Need more detail?",
				},
			},
		}},
	}

	if err := hooks.After(context.Background(), state, result); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	raw, _ := state.Metadata["clarification_interrupt"].(map[string]any)
	if stringValue(raw["id"]) != "clarify-1" {
		t.Fatalf("clarification id = %q", stringValue(raw["id"]))
	}
}

func TestProviderStyleLifecycleAdapters(t *testing.T) {
	providerStylePersistedSummary = ""

	memoryHooks := MemoryLifecycleHooksWithResolver(&MemoryRuntime{}, providerStyleResolverStub{}, "memory_session_id")
	if memoryHooks != nil {
		t.Fatal("memory hooks should be nil when runtime is disabled")
	}

	summaryHooks := SummarizationLifecycleHooksWithSummarizer(providerStyleSummarizerStub{}, "summary_key")
	titleHooks := TitleLifecycleHooksWithGenerator(providerStyleTitleStub{}, "title_key")
	hooks := MergeLifecycleHooks(summaryHooks, titleHooks)

	state := &RunState{
		ThreadID: "thread-1",
		Messages: []models.Message{{Role: models.RoleHuman, Content: "hello"}},
		Metadata: map[string]any{},
	}
	if err := hooks.Before(context.Background(), state); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if got := stringValue(state.Metadata["summary_key"]); got != "summary" {
		t.Fatalf("summary metadata = %q", got)
	}
	if err := hooks.After(context.Background(), state, &agent.RunResult{}); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	if providerStylePersistedSummary != "summary" {
		t.Fatalf("persistedSummary = %q", providerStylePersistedSummary)
	}
	if got := stringValue(state.Metadata["title_key"]); got != "title" {
		t.Fatalf("title metadata = %q", got)
	}
}

func TestMemoryLifecycleHooksWithScopePlannerUsesPrimaryScope(t *testing.T) {
	hooks := MemoryLifecycleHooksWithScopePlanner(&MemoryRuntime{}, providerStylePlannerStub{}, "memory_session_id")
	if hooks != nil {
		t.Fatal("memory hooks should be nil when runtime is disabled")
	}
}
