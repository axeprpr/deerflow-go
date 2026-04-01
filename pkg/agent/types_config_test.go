package agent

import (
	"context"
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
		"write_file",
		"str_replace",
		"present_files",
		"ask_clarification",
		"task",
		"web_search",
		"web_fetch",
		"image_search",
		"glob",
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
	want := []string{"ask_clarification", "bash", "ls", "present_files", "read_file", "str_replace", "task", "write_file"}
	if len(got) != len(want) {
		t.Fatalf("coder tools=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("coder tools=%v want=%v", got, want)
		}
	}
	for _, name := range got {
		if name == "glob" {
			t.Fatalf("coder tools unexpectedly include glob: %v", got)
		}
	}

	cfg = AgentConfig{Tools: registry}
	if err := ApplyAgentType(&cfg, AgentTypeResearch); err != nil {
		t.Fatalf("ApplyAgentType(researcher) error = %v", err)
	}
	got = registryToolNames(cfg.Tools)
	want = []string{"ask_clarification", "image_search", "ls", "present_files", "read_file", "task", "web_fetch", "web_search"}
	if len(got) != len(want) {
		t.Fatalf("researcher tools=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("researcher tools=%v want=%v", got, want)
		}
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
