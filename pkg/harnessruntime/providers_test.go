package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

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
