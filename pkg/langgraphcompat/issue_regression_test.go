package langgraphcompat

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestIssue2143And2125CustomSkillUsesMountedSkillPath(t *testing.T) {
	s := newFileCompatTestServer(t)
	writeGatewaySkill(t, s.dataRoot, "custom", "scenic-spot-analytics", `---
name: scenic-spot-analytics
description: Query scenic spot analytics data
---

# Scenic Spot Analytics
`)

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig() error = %v", err)
	}

	if !strings.Contains(cfg.SystemPrompt, "/mnt/skills/custom/scenic-spot-analytics/SKILL.md") {
		t.Fatalf("system prompt missing mounted custom skill path: %q", cfg.SystemPrompt)
	}
	if strings.Contains(cfg.SystemPrompt, "/mnt/user-data/scenic-spot-analytics") ||
		strings.Contains(cfg.SystemPrompt, "/mnt/user-data/uploads/scenic-spot-analytics") ||
		strings.Contains(cfg.SystemPrompt, "/mnt/user-data/workspace/scenic-spot-analytics") {
		t.Fatalf("system prompt should not route custom skill through user-data: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Query scenic spot analytics data") {
		t.Fatalf("system prompt missing custom skill description: %q", cfg.SystemPrompt)
	}

	paths := s.runtimeSkillPaths()
	if got := paths["scenic-spot-analytics"]; got != "/mnt/skills/custom/scenic-spot-analytics/SKILL.md" {
		t.Fatalf("runtimeSkillPaths[scenic-spot-analytics]=%q", got)
	}
}

func TestIssue2139DefaultRuntimeExposesWebSearchTools(t *testing.T) {
	runtime := harnessruntime.NewDefaultToolRuntime(nil, clarification.NewManager(4), nil)
	if runtime == nil || runtime.Registry() == nil {
		t.Fatal("runtime tool registry is not available")
	}

	for _, name := range []string{"web_search", "web_fetch", "image_search"} {
		if runtime.Registry().Get(name) == nil {
			t.Fatalf("default runtime tool surface missing %s", name)
		}
	}
}
