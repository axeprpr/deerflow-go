package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
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
}
