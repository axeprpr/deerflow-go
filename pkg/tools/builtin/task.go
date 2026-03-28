package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TaskTool(pool *agent.SubagentPool) models.Tool {
	return models.Tool{
		Name:        "task",
		Description: "Launch a bounded subagent task and return its final output.",
		Groups:      []string{"builtin", "agent"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task":          map[string]any{"type": "string"},
				"system_prompt": map[string]any{"type": "string"},
				"tools":         map[string]any{"type": "array"},
			},
			"required": []any{"task"},
		},
		Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
			if pool == nil {
				return models.ToolResult{}, fmt.Errorf("subagent pool is required")
			}

			task, _ := call.Arguments["task"].(string)
			systemPrompt, _ := call.Arguments["system_prompt"].(string)

			output, err := pool.Execute(ctx, strings.TrimSpace(task), agent.SubagentConfig{
				Name:         fmt.Sprintf("subagent-%d", time.Now().UTC().UnixNano()),
				SystemPrompt: strings.TrimSpace(systemPrompt),
				AllowedTools: stringList(call.Arguments["tools"]),
			})
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.Name,
				Content:  output,
			}, err
		},
	}
}

func stringList(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	default:
		return nil
	}
}
