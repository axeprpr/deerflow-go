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
	if err := tools.EnsureThreadDataDirs(ctx); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("prepare thread directories failed: %w", err)
	}
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
		Description: "Execute a bash command in a Linux environment.\n\n- Use `python` to run Python code.\n- Prefer a thread-local virtual environment in `/mnt/user-data/workspace/.venv`.\n- Use `python -m pip` inside the virtual environment to install Python packages.",
		Groups:      []string{"builtin"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string", "description": "Explain why you are running this command in short words. ALWAYS PROVIDE THIS PARAMETER FIRST."},
				"command":     map[string]any{"type": "string", "description": "The bash command to execute. Always use absolute paths for files and directories."},
				"timeout":     map[string]any{"type": "number", "description": "Timeout in seconds (default 60)"},
			},
			"required": []any{"description", "command"},
		},
		Handler: BashHandler,
	}
}
