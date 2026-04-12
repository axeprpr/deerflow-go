package harness

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
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

func TestRunnerRunExecutesManagedSandboxCallbacks(t *testing.T) {
	var heartbeats, releases int
	runner := NewRunner(NewFactory(RuntimeDeps{
		LLMProvider: runnerTestProvider{
			message: "runner ok",
		},
		SandboxRuntime: runnerManagedSandboxRuntime{
			bind: func(AgentRequest) (SandboxBinding, error) {
				return SandboxBinding{
					Heartbeat: func() error {
						heartbeats++
						return nil
					},
					Release: func() error {
						releases++
						return nil
					},
				}, nil
			},
		},
	}))

	execution, err := runner.Prepare(RunRequest{
		Agent: AgentRequest{
			Spec: AgentSpec{SystemPrompt: "system"},
			Features: FeatureSet{
				Sandbox: true,
			},
		},
		SessionID: "thread-1",
		Messages:  []models.Message{{Role: models.RoleHuman, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	if _, err := execution.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if heartbeats == 0 {
		t.Fatal("heartbeat callback was not called")
	}
	if releases != 1 {
		t.Fatalf("releases = %d, want 1", releases)
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

type runnerManagedSandboxRuntime struct {
	bind func(AgentRequest) (SandboxBinding, error)
}

func (r runnerManagedSandboxRuntime) Provider() SandboxProvider {
	return nil
}

func (r runnerManagedSandboxRuntime) Resolve(req AgentRequest) (*sandbox.Sandbox, error) {
	binding, err := r.Bind(req)
	if err != nil {
		return nil, err
	}
	return binding.Sandbox, nil
}

func (r runnerManagedSandboxRuntime) Bind(req AgentRequest) (SandboxBinding, error) {
	if r.bind == nil {
		return SandboxBinding{}, nil
	}
	return r.bind(req)
}

func (r runnerManagedSandboxRuntime) Close() error {
	return nil
}
