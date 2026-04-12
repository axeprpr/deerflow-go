package langgraphcompat

import (
	"strings"
	"testing"
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
