package harness

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestRunnerPrepareAndRun(t *testing.T) {
	runner := NewRunner(NewFactory(RuntimeDeps{
		LLMProvider: runnerTestProvider{
			message: "runner ok",
		},
	}))

	execution, err := runner.Prepare(RunRequest{
		Agent: AgentRequest{
			Spec: AgentSpec{
				SystemPrompt: "system",
			},
		},
		SessionID: "thread-1",
		Messages: []models.Message{{
			Role:    models.RoleHuman,
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if execution == nil {
		t.Fatal("Prepare() returned nil execution")
	}

	result, err := execution.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result == nil {
		t.Fatal("Run() returned nil result")
	}
	if got := result.FinalOutput; got != "runner ok" {
		t.Fatalf("FinalOutput = %q, want %q", got, "runner ok")
	}
}

type runnerTestProvider struct {
	message string
}

func (p runnerTestProvider) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{
		Message: models.Message{
			Role:    models.RoleAI,
			Content: p.message,
		},
		Stop: "stop",
	}, nil
}

func (p runnerTestProvider) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{
		Message: &models.Message{
			Role:    models.RoleAI,
			Content: p.message,
		},
		Stop: "stop",
		Done: true,
	}
	close(ch)
	return ch, nil
}
