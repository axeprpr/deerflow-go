package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestModeToolRuntimeBackgroundRemovesInteractiveTools(t *testing.T) {
	registry := tools.NewRegistry()
	for _, name := range []string{"ls", "ask_clarification", "task"} {
		if err := registry.Register(models.Tool{
			Name:        name,
			Description: name,
			Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
				return models.ToolResult{}, nil
			},
		}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}

	runtime := modeToolRuntime(ExecutionModeBackground, harness.NewStaticToolRuntime(registry, nil, nil))
	if runtime == nil || runtime.Registry() == nil {
		t.Fatal("expected filtered runtime")
	}
	got := runtime.Registry().List()
	if len(got) != 1 || got[0].Name != "ls" {
		t.Fatalf("tools = %#v", got)
	}
}

type sandboxRuntimeStub struct {
	lastRequest harness.AgentRequest
}

func (s *sandboxRuntimeStub) Provider() harness.SandboxProvider { return nil }
func (s *sandboxRuntimeStub) Resolve(req harness.AgentRequest) (*tools.Sandbox, error) {
	s.lastRequest = req
	return nil, nil
}
func (s *sandboxRuntimeStub) Close() error { return nil }

func TestModeSandboxRuntimeStrictForcesSandbox(t *testing.T) {
	base := &sandboxRuntimeStub{}
	runtime := modeSandboxRuntime(ExecutionModeStrict, base)
	if runtime == nil {
		t.Fatal("expected strict sandbox runtime")
	}
	if _, err := runtime.Resolve(harness.AgentRequest{}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !base.lastRequest.Features.Sandbox {
		t.Fatal("strict mode should force sandbox feature")
	}
}
