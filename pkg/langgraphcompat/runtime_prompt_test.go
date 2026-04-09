package langgraphcompat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
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
	if !strings.Contains(cfg.SystemPrompt, "<response_style>") {
		t.Fatalf("system prompt missing response style section: %q", cfg.SystemPrompt)
	}
	if strings.Contains(cfg.SystemPrompt, "<clarification_system>") {
		t.Fatalf("system prompt unexpectedly included custom clarification workflow: %q", cfg.SystemPrompt)
	}
	if strings.Contains(cfg.SystemPrompt, "<subagent_system>") {
		t.Fatalf("system prompt unexpectedly included subagent guidance: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "<citations>") {
		t.Fatalf("system prompt missing citations guidance: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "[citation:TITLE](URL)") {
		t.Fatalf("system prompt missing citation link format guidance: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Including Images and Mermaid") {
		t.Fatalf("system prompt missing image/mermaid reminder: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Multi-task") {
		t.Fatalf("system prompt missing multi-task reminder: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Language Consistency") {
		t.Fatalf("system prompt missing critical reminders: %q", cfg.SystemPrompt)
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
	if !strings.Contains(cfg.SystemPrompt, "then use `present_file`") {
		t.Fatalf("system prompt missing ACP present_file guidance: %q", cfg.SystemPrompt)
	}
}

func TestResolveRunConfigIncludesEnabledSkillsPrompt(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "public", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: demo-skill
description: Demo workflow
---

# Demo Skill
`), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	s := &Server{
		dataRoot: root,
		tools:    newRuntimeToolRegistry(t),
	}

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "<skill_system>") {
		t.Fatalf("system prompt missing skill system section: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "/mnt/skills/public/demo-skill/SKILL.md") {
		t.Fatalf("system prompt missing skill location: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Demo workflow") {
		t.Fatalf("system prompt missing skill description: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Progressive Loading Pattern") {
		t.Fatalf("system prompt missing progressive loading guidance: %q", cfg.SystemPrompt)
	}
}

func TestSkillsPromptMatchesUpstreamProgressiveLoadingShape(t *testing.T) {
	root := t.TempDir()
	for _, skill := range []struct {
		category    string
		name        string
		description string
	}{
		{category: "public", name: "z-skill", description: "Last public skill"},
		{category: "public", name: "a-skill", description: "First public skill"},
	} {
		skillDir := filepath.Join(root, "skills", skill.category, skill.name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir skill dir: %v", err)
		}
		body := "---\nname: " + skill.name + "\ndescription: " + skill.description + "\n---\n\n# Skill\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write skill: %v", err)
		}
	}
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	s := &Server{dataRoot: root}
	prompt := s.skillsPrompt()
	if !strings.Contains(prompt, "**Skills are located at:** /mnt/skills") {
		t.Fatalf("skills prompt missing upstream skills location: %q", prompt)
	}
	if !strings.Contains(prompt, "1. When a user query matches a skill's use case") {
		t.Fatalf("skills prompt missing progressive loading step 1: %q", prompt)
	}
	if strings.Contains(prompt, "treat `frontend-design` as the default skill to load first") {
		t.Fatalf("skills prompt unexpectedly included frontend special case: %q", prompt)
	}
	if strings.Index(prompt, "<name>a-skill</name>") > strings.Index(prompt, "<name>z-skill</name>") {
		t.Fatalf("skills prompt ordering is not stable: %q", prompt)
	}
}

func TestResolveRunConfigDoesNotInjectFrontendDesignSpecialCase(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "public", "frontend-design")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: frontend-design
description: Build pages and interfaces
---

# Frontend Design
`), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	s := &Server{
		dataRoot: root,
		tools:    newRuntimeToolRegistry(t),
	}

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if strings.Contains(cfg.SystemPrompt, "treat `frontend-design` as the default skill to load first") {
		t.Fatalf("system prompt unexpectedly included frontend-design special case: %q", cfg.SystemPrompt)
	}
}

func TestResolveRunConfigIncludesUpstreamCitationsAndReminders(t *testing.T) {
	s := &Server{
		tools: newRuntimeToolRegistry(t),
	}

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "<citations>") {
		t.Fatalf("system prompt missing citations section: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Always include citations when using web search results") {
		t.Fatalf("system prompt missing citations guidance: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Including Images and Mermaid") {
		t.Fatalf("system prompt missing image guidance: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Multi-task") {
		t.Fatalf("system prompt missing multi-task guidance: %q", cfg.SystemPrompt)
	}
}

func TestResolveRunConfigKeepsBuiltinAgentBasePrompt(t *testing.T) {
	s := &Server{
		tools: newRuntimeToolRegistry(t),
	}

	cfg, err := s.resolveRunConfig(runConfig{AgentType: agent.AgentTypeCoder}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, agent.GetAgentTypeConfig(agent.AgentTypeCoder).SystemPrompt) {
		t.Fatalf("system prompt missing builtin coder prompt: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "/mnt/user-data/outputs") {
		t.Fatalf("system prompt missing runtime guidance: %q", cfg.SystemPrompt)
	}
}

func TestResolveRunConfigInjectsUserProfileForBuiltinAgents(t *testing.T) {
	s := &Server{
		tools:       newRuntimeToolRegistry(t),
		userProfile: "Prefers terse answers and Go examples.",
	}

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "USER.md:") {
		t.Fatalf("system prompt missing user profile header: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Prefers terse answers and Go examples.") {
		t.Fatalf("system prompt missing user profile content: %q", cfg.SystemPrompt)
	}
}

func TestResolveRunConfigIncludesSubagentPromptWhenEnabled(t *testing.T) {
	s := &Server{
		tools: newRuntimeToolRegistry(t),
	}

	cfg, err := s.resolveRunConfig(runConfig{}, map[string]any{
		"subagent_enabled":         true,
		"max_concurrent_subagents": 5,
	})
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "<subagent_system>") {
		t.Fatalf("system prompt missing subagent guidance: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "at most 5 `task` calls") {
		t.Fatalf("system prompt missing subagent concurrency limit: %q", cfg.SystemPrompt)
	}
}
