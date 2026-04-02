package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func BashHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	args := call.Arguments

	cmd, ok := args["command"].(string)
	if !ok || strings.TrimSpace(cmd) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("command is required")
	}

	timeout := 60 * time.Second
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	cmd = tools.ResolveVirtualCommand(ctx, cmd)
	workdir := tools.ResolveWorkingDirectory(ctx)
	if strings.TrimSpace(workdir) != "" {
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("prepare workspace failed: %w", err)
		}
	}
	result, err := sandbox.ExecDirectInDir(ctx, cmd, workdir, timeout)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("bash failed: %w", err)
	}

	output := &BashOutput{
		Stdout:   tools.MaskLocalPaths(ctx, result.Stdout()),
		Stderr:   tools.MaskLocalPaths(ctx, result.Stderr()),
		ExitCode: result.ExitCode(),
	}
	data, _ := json.Marshal(output)
	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: string(data)}, nil
}

type BashOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func BashTool() models.Tool {
	return models.Tool{
		Name:        "bash",
		Description: "Execute shell commands. Returns stdout, stderr, and exit code as JSON.",
		Groups:      []string{"builtin"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "Shell command to execute"},
				"timeout": map[string]any{"type": "number", "description": "Timeout in seconds (default 60)"},
			},
			"required": []any{"command"},
		},
		Handler: BashHandler,
	}
}
