package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestModeProfileResolverLeavesInteractiveProfileUntouched(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(models.Tool{
		Name:        "ls",
		Description: "ls",
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{}, nil
		},
	})
	baseToolRuntime := harness.NewStaticToolRuntime(registry, nil, nil)
	baseSandboxRuntime := &sandboxRuntimeStub{}
	base := harness.RuntimeProfile{
		ToolRuntime:    baseToolRuntime,
		SandboxRuntime: baseSandboxRuntime,
	}

	resolved := NewModeProfileResolver().ResolveProfile(base, harness.AgentRequest{
		Spec: harness.AgentSpec{ExecutionMode: "interactive"},
	})
	if resolved.ToolRuntime != baseToolRuntime {
		t.Fatal("interactive mode should retain base tool runtime")
	}
	if resolved.SandboxRuntime != baseSandboxRuntime {
		t.Fatal("interactive mode should retain base sandbox runtime")
	}
}

func TestModeProfileResolverAppliesBackgroundToolRestrictions(t *testing.T) {
	registry := tools.NewRegistry()
	for _, name := range []string{"ls", "ask_clarification", "task"} {
		_ = registry.Register(models.Tool{
			Name:        name,
			Description: name,
			Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
				return models.ToolResult{}, nil
			},
		})
	}
	base := harness.RuntimeProfile{
		ToolRuntime: harness.NewStaticToolRuntime(registry, nil, nil),
	}

	resolved := NewModeProfileResolver().ResolveProfile(base, harness.AgentRequest{
		Spec: harness.AgentSpec{ExecutionMode: "background"},
	})
	got := resolved.ToolRuntime.Registry().List()
	if len(got) != 1 || got[0].Name != "ls" {
		t.Fatalf("tools = %#v", got)
	}
}
