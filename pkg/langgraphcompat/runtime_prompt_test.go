package langgraphcompat

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestResolveRunConfigIncludesWorkingDirectoryGuidance(t *testing.T) {
	s := &Server{
		tools: newRuntimeToolRegistry(t),
	}

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "/mnt/user-data/outputs") {
		t.Fatalf("system prompt missing outputs guidance: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "presented using `present_file` tool") {
		t.Fatalf("system prompt missing present_file guidance: %q", cfg.SystemPrompt)
	}
	if strings.Contains(cfg.SystemPrompt, "ACP Agent Tasks") {
		t.Fatalf("system prompt unexpectedly included ACP guidance: %q", cfg.SystemPrompt)
	}
}

func TestResolveRunConfigIncludesACPGuidanceWhenToolConfigured(t *testing.T) {
	registry := newRuntimeToolRegistry(t)
	if err := registry.Register(models.Tool{
		Name:   "invoke_acp_agent",
		Groups: []string{"builtin", "agent"},
		Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{}, nil
		},
	}); err != nil {
		t.Fatalf("register invoke_acp_agent: %v", err)
	}

	s := &Server{
		tools: registry,
	}

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "ACP Agent Tasks (`invoke_acp_agent`)") {
		t.Fatalf("system prompt missing ACP section: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "/mnt/acp-workspace/") {
		t.Fatalf("system prompt missing ACP workspace guidance: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "copy from `/mnt/acp-workspace/<file>` to `/mnt/user-data/outputs/<file>`") {
		t.Fatalf("system prompt missing ACP delivery guidance: %q", cfg.SystemPrompt)
	}
}
