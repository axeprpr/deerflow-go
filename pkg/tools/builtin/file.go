package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func ReadFileHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	args := call.Arguments
	path, ok := args["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required")
	}
	path = tools.ResolveVirtualPath(ctx, path)

	data, err := os.ReadFile(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("read failed: %w", err)
	}

	if limit, ok := args["limit"].(float64); ok && limit > 0 && int(limit) < len(data) {
		data = data[:int(limit)]
	}

	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: string(data)}, nil
}

func WriteFileHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	args := call.Arguments
	path, ok := args["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required")
	}
	path = tools.ResolveVirtualPath(ctx, path)
	content, ok := args["content"].(string)
	if !ok {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("content is required")
	}
	appendMode, _ := args["append"].(bool)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("mkdir failed: %w", err)
	}
	if appendMode {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %w", err)
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %w", err)
		}
	} else if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %w", err)
	}

	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: fmt.Sprintf("Written %d bytes to %s", len(content), path)}, nil
}

func GlobHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	args := call.Arguments
	pattern, ok := args["pattern"].(string)
	if !ok || strings.TrimSpace(pattern) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("pattern is required")
	}
	pattern = tools.ResolveVirtualPath(ctx, pattern)

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("glob failed: %w", err)
	}

	data, _ := json.Marshal(matches)
	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: string(data)}, nil
}

func GlobTool() models.Tool {
	return models.Tool{
		Name:        "glob",
		Description: "List files matching a glob pattern.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Glob pattern (e.g. *.go)"},
				"root":    map[string]any{"type": "string", "description": "Root directory (default .)"},
			},
			"required": []any{"pattern"},
		},
		Handler: GlobHandler,
	}
}

func ReadFileTool() models.Tool {
	return models.Tool{
		Name:        "read_file",
		Description: "Read the contents of a file.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]any{"type": "string", "description": "File path to read"},
				"limit": map[string]any{"type": "number", "description": "Maximum bytes to read"},
			},
			"required": []any{"path"},
		},
		Handler: ReadFileHandler,
	}
}

func WriteFileTool() models.Tool {
	return models.Tool{
		Name:        "write_file",
		Description: "Write content to a file.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path to write"},
				"content": map[string]any{"type": "string", "description": "Content to write"},
				"append":  map[string]any{"type": "boolean", "description": "Append instead of overwrite"},
			},
			"required": []any{"path", "content"},
		},
		Handler: WriteFileHandler,
	}
}

// FileTools returns all file operation tools.
func FileTools() []models.Tool {
	return []models.Tool{
		ReadFileTool(),
		WriteFileTool(),
		GlobTool(),
	}
}
