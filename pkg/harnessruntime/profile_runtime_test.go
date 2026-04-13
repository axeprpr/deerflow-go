package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
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
func (s *sandboxRuntimeStub) Resolve(req harness.AgentRequest) (sandbox.Session, error) {
	s.lastRequest = req
	return nil, nil
}
func (s *sandboxRuntimeStub) Close() error { return nil }

type managedSandboxRuntimeStub struct {
	lastRequest harness.AgentRequest
}

func (s *managedSandboxRuntimeStub) Provider() harness.SandboxProvider { return nil }
func (s *managedSandboxRuntimeStub) Resolve(req harness.AgentRequest) (sandbox.Session, error) {
	binding, err := s.Bind(req)
	if err != nil {
		return nil, err
	}
	return binding.Sandbox, nil
}
func (s *managedSandboxRuntimeStub) Bind(req harness.AgentRequest) (harness.SandboxBinding, error) {
	s.lastRequest = req
	return harness.SandboxBinding{
		Sandbox:   &sandbox.Sandbox{},
		Heartbeat: func() error { return nil },
		Release:   func() error { return nil },
	}, nil
}
func (s *managedSandboxRuntimeStub) Close() error { return nil }

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

func TestModeSandboxRuntimeStrictPreservesManagedBindings(t *testing.T) {
	base := &managedSandboxRuntimeStub{}
	runtime := modeSandboxRuntime(ExecutionModeStrict, base)
	managed, ok := runtime.(harness.ManagedSandboxRuntime)
	if !ok {
		t.Fatalf("runtime does not preserve managed sandbox runtime: %#v", runtime)
	}
	binding, err := managed.Bind(harness.AgentRequest{})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if binding.Heartbeat == nil || binding.Release == nil || binding.Sandbox == nil {
		t.Fatalf("binding = %#v", binding)
	}
	if !base.lastRequest.Features.Sandbox {
		t.Fatal("strict mode should force sandbox feature")
	}
}
