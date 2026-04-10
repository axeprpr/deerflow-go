package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestApplyAgentTypeUsesUpdatedBuiltinFileToolsets(t *testing.T) {
	registry := tools.NewRegistry()
	for _, name := range []string{
		"bash",
		"ls",
		"read_file",
		"glob",
		"grep",
		"write_file",
		"str_replace",
		"present_files",
		"ask_clarification",
		"task",
		"web_search",
		"web_fetch",
		"image_search",
	} {
		if err := registry.Register(models.Tool{Name: name, Handler: func(_ context.Context, _ models.ToolCall) (models.ToolResult, error) {
			return models.ToolResult{}, nil
		}}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}

	cfg := AgentConfig{Tools: registry}
	if err := ApplyAgentType(&cfg, AgentTypeCoder); err != nil {
		t.Fatalf("ApplyAgentType(coder) error = %v", err)
	}

	got := registryToolNames(cfg.Tools)
	want := []string{"bash", "ls", "read_file", "glob", "grep", "write_file", "str_replace", "present_files", "ask_clarification", "task"}
	if len(got) != len(want) {
		t.Fatalf("coder tools=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("coder tools=%v want=%v", got, want)
		}
	}
	cfg = AgentConfig{Tools: registry}
	if err := ApplyAgentType(&cfg, AgentTypeResearch); err != nil {
		t.Fatalf("ApplyAgentType(researcher) error = %v", err)
	}
	got = registryToolNames(cfg.Tools)
	want = []string{"ls", "read_file", "glob", "grep", "present_files", "ask_clarification", "task", "web_search", "web_fetch", "image_search"}
	if len(got) != len(want) {
		t.Fatalf("researcher tools=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("researcher tools=%v want=%v", got, want)
		}
	}
}

func TestBuiltinAgentPromptsEmphasizeDirectExecution(t *testing.T) {
	if strings.Contains(generalPurposeSystemPrompt, "<execution_mode>") {
		t.Fatalf("general prompt unexpectedly includes custom execution mode: %q", generalPurposeSystemPrompt)
	}
	if strings.Contains(coderSystemPrompt, "<execution_mode>") {
		t.Fatalf("coder prompt unexpectedly includes custom execution mode: %q", coderSystemPrompt)
	}
	if !strings.Contains(generalPurposeSystemPrompt, "<role>") || !strings.Contains(coderSystemPrompt, "<role>") {
		t.Fatalf("builtin prompts should remain role-only: general=%q coder=%q", generalPurposeSystemPrompt, coderSystemPrompt)
	}
}

func registryToolNames(registry *tools.Registry) []string {
	list := registry.List()
	out := make([]string, 0, len(list))
	for _, tool := range list {
		out = append(out, tool.Name)
	}
	return out
}
