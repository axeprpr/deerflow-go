package langgraphcompat

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestResolveAgentToolRegistryUsesUpdatedFileToolMappings(t *testing.T) {
	registry := tools.NewRegistry()
	for _, tool := range []models.Tool{
		{Name: "bash", Groups: []string{"builtin"}, Handler: noopToolHandler},
		{Name: "ls", Groups: []string{"builtin", "file_ops"}, Handler: noopToolHandler},
		{Name: "read_file", Groups: []string{"builtin", "file_ops"}, Handler: noopToolHandler},
		{Name: "glob", Groups: []string{"builtin", "file_ops"}, Handler: noopToolHandler},
		{Name: "grep", Groups: []string{"builtin", "file_ops"}, Handler: noopToolHandler},
		{Name: "write_file", Groups: []string{"builtin", "file_ops"}, Handler: noopToolHandler},
		{Name: "str_replace", Groups: []string{"builtin", "file_ops"}, Handler: noopToolHandler},
		{Name: "present_files", Groups: []string{"builtin", "file_ops"}, Handler: noopToolHandler},
		{Name: "ask_clarification", Groups: []string{"builtin", "interaction"}, Handler: noopToolHandler},
		{Name: "task", Groups: []string{"agent"}, Handler: noopToolHandler},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Name, err)
		}
	}

	t.Run("file", func(t *testing.T) {
		got := registryNames(resolveAgentToolRegistry(registry, []string{"file"}))
		want := []string{"ls", "read_file", "glob", "grep", "write_file", "str_replace", "present_files", "ask_clarification"}
		assertToolNames(t, got, want)
	})

	t.Run("file read", func(t *testing.T) {
		got := registryNames(resolveAgentToolRegistry(registry, []string{"file:read"}))
		want := []string{"ls", "read_file", "glob", "grep", "ask_clarification"}
		assertToolNames(t, got, want)
	})

	t.Run("file write", func(t *testing.T) {
		got := registryNames(resolveAgentToolRegistry(registry, []string{"file:write"}))
		want := []string{"write_file", "str_replace", "present_files", "ask_clarification"}
		assertToolNames(t, got, want)
	})
}

func noopToolHandler(_ context.Context, _ models.ToolCall) (models.ToolResult, error) {
	return models.ToolResult{}, nil
}

func registryNames(registry *tools.Registry) []string {
	list := registry.List()
	out := make([]string, 0, len(list))
	for _, tool := range list {
		out = append(out, tool.Name)
	}
	return out
}

func assertToolNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tool names=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool names=%v want=%v", got, want)
		}
	}
}
