package langgraphcompat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
	toolctx "github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestSetupAgentToolCreatesAgentFromRuntimeContext(t *testing.T) {
	s, _ := newCompatTestServer(t)
	tool := s.setupAgentTool()

	ctx := toolctx.WithRuntimeContext(context.Background(), map[string]any{
		"agent_name": "code-reviewer",
	})
	result, err := tool.Handler(ctx, models.ToolCall{
		ID:   "call-setup",
		Name: "setup_agent",
		Arguments: map[string]any{
			"soul":        "# SOUL\nBe rigorous.",
			"description": "Reviews code changes carefully",
		},
	})
	if err != nil {
		t.Fatalf("setup_agent error: %v", err)
	}
	if result.Status != models.CallStatusCompleted {
		t.Fatalf("result status=%q", result.Status)
	}

	s.uiStateMu.RLock()
	created, ok := s.getAgentsLocked()["code-reviewer"]
	s.uiStateMu.RUnlock()
	if !ok {
		t.Fatal("expected agent to be created")
	}
	if created.Description != "Reviews code changes carefully" {
		t.Fatalf("description=%q", created.Description)
	}
	if created.Soul != "# SOUL\nBe rigorous." {
		t.Fatalf("soul=%q", created.Soul)
	}

	if _, err := os.Stat(filepath.Join(s.agentDir("code-reviewer"), "SOUL.md")); err != nil {
		t.Fatalf("expected SOUL.md to be written: %v", err)
	}
}

func TestSetupAgentToolRequiresRuntimeAgentName(t *testing.T) {
	s, _ := newCompatTestServer(t)
	tool := s.setupAgentTool()

	_, err := tool.Handler(context.Background(), models.ToolCall{
		ID:   "call-setup",
		Name: "setup_agent",
		Arguments: map[string]any{
			"soul":        "# SOUL\nBe rigorous.",
			"description": "Reviews code changes carefully",
		},
	})
	if err == nil {
		t.Fatal("expected missing runtime agent name error")
	}
	if !strings.Contains(err.Error(), "agent name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
