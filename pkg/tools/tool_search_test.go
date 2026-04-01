package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestDeferredToolRegistrySearchSelect(t *testing.T) {
	registry := NewDeferredToolRegistry([]models.Tool{
		{Name: "github.search_repos", Description: "Search repositories", Handler: noopToolHandler},
		{Name: "slack.send_message", Description: "Send Slack messages", Handler: noopToolHandler},
	})

	got := registry.Search("select:slack.send_message,github.search_repos")
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].Name != "slack.send_message" {
		t.Fatalf("first=%q want slack.send_message", got[0].Name)
	}
}

func TestDeferredToolRegistrySearchRequiredName(t *testing.T) {
	registry := NewDeferredToolRegistry([]models.Tool{
		{Name: "slack.send_message", Description: "Send Slack messages", Handler: noopToolHandler},
		{Name: "slack.list_channels", Description: "List channels", Handler: noopToolHandler},
		{Name: "github.search_repos", Description: "Search repositories", Handler: noopToolHandler},
	})

	got := registry.Search("+slack send")
	if len(got) == 0 {
		t.Fatal("expected matches")
	}
	if got[0].Name != "slack.send_message" {
		t.Fatalf("first=%q want slack.send_message", got[0].Name)
	}
}

func TestDeferredToolRegistrySearchRegexFallsBackToEscapedPattern(t *testing.T) {
	registry := NewDeferredToolRegistry([]models.Tool{
		{Name: "regex(test)", Description: "Search repositories", Handler: noopToolHandler},
	})

	got := registry.Search("regex(test")
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
}

func TestDeferredToolSearchToolReturnsSchemas(t *testing.T) {
	registry := NewDeferredToolRegistry([]models.Tool{
		{
			Name:        "github.search_repos",
			Description: "Search repositories",
			InputSchema: map[string]any{"type": "object"},
			Handler:     noopToolHandler,
		},
	})
	var activated []models.Tool
	tool := DeferredToolSearchTool(registry.Search, func(items []models.Tool) {
		activated = append(activated, items...)
	})

	result, err := tool.Handler(context.Background(), models.ToolCall{
		ID:        "call-1",
		Name:      "tool_search",
		Arguments: map[string]any{"query": "github"},
		Status:    models.CallStatusPending,
	})
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if len(activated) != 1 || activated[0].Name != "github.search_repos" {
		t.Fatalf("activated=%v", activated)
	}
	if !strings.Contains(result.Content, "\"github.search_repos\"") {
		t.Fatalf("content=%q", result.Content)
	}

	var payload []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len=%d want 1", len(payload))
	}
	if payload[0]["type"] != "function" {
		t.Fatalf("type=%v want function", payload[0]["type"])
	}
	fn, ok := payload[0]["function"].(map[string]any)
	if !ok {
		t.Fatalf("function=%T want object", payload[0]["function"])
	}
	if fn["name"] != "github.search_repos" {
		t.Fatalf("function.name=%v want github.search_repos", fn["name"])
	}
	if fn["parameters"] == nil {
		t.Fatal("function.parameters should not be nil")
	}
}

func noopToolHandler(_ context.Context, _ models.ToolCall) (models.ToolResult, error) {
	return models.ToolResult{}, nil
}
