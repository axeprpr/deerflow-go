package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	path = resolveReadableFilePath(ctx, path)

	data, err := os.ReadFile(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("read failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}

	if limit, ok := args["limit"].(float64); ok && limit > 0 && int(limit) < len(data) {
		data = data[:int(limit)]
	}
	if startLine, endLine, ok := resolveLineRange(args); ok {
		data = []byte(sliceContentLines(string(data), startLine, endLine))
	}

	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: string(data)}, nil
}

func resolveLineRange(args map[string]any) (int, int, bool) {
	start, startOK := intArg(args["start_line"])
	end, endOK := intArg(args["end_line"])
	if !startOK && !endOK {
		return 0, 0, false
	}
	if !startOK {
		start = 1
	}
	if !endOK {
		end = int(^uint(0) >> 1)
	}
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	return start, end, true
}

func intArg(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		parsed, err := strconv.Atoi(value.String())
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func sliceContentLines(content string, startLine, endLine int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if startLine > len(lines) {
		return ""
	}
	startIdx := startLine - 1
	endIdx := endLine
	if endIdx > len(lines) {
		endIdx = len(lines)
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx < startIdx {
		endIdx = startIdx
	}
	return strings.Join(lines[startIdx:endIdx], "\n")
}

func resolveReadableFilePath(ctx context.Context, path string) string {
	path = strings.TrimSpace(path)
	resolved := tools.ResolveVirtualPath(ctx, path)
	if !shouldPreferMarkdownCompanion(path) {
		return resolved
	}

	companion := strings.TrimSuffix(resolved, filepath.Ext(resolved)) + ".md"
	info, err := os.Stat(companion)
	if err != nil || !info.Mode().IsRegular() {
		return resolved
	}
	return companion
}

func shouldPreferMarkdownCompanion(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if !strings.HasPrefix(path, "/mnt/user-data/uploads/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf", ".ppt", ".pptx", ".xls", ".xlsx", ".doc", ".docx", ".csv", ".tsv", ".json", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func WriteFileHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	args := call.Arguments
	requestedPath, ok := args["path"].(string)
	if !ok || strings.TrimSpace(requestedPath) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required")
	}
	path := tools.ResolveVirtualPath(ctx, requestedPath)
	if err := tools.ValidateWritableToolPath(ctx, requestedPath, path); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, err
	}
	content, ok := args["content"].(string)
	if !ok {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("content is required")
	}
	appendMode, _ := args["append"].(bool)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("mkdir failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	if appendMode {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
		}
	} else if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}

	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "OK"}, nil
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
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("glob failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	for i := range matches {
		matches[i] = tools.MaskLocalPaths(ctx, matches[i])
	}

	data, _ := json.Marshal(matches)
	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: string(data)}, nil
}

func LsHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	args := call.Arguments
	path, ok := args["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required")
	}
	path = tools.ResolveVirtualPath(ctx, path)

	info, err := os.Stat(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("list failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	if !info.IsDir() {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is not a directory")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("list failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	if len(entries) == 0 {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "(empty)"}, nil
	}

	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: renderDirTree(path, entries, 0)}, nil
}

func renderDirTree(root string, entries []os.DirEntry, depth int) string {
	lines := make([]string, 0, len(entries))
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.IsDir() != right.IsDir() {
			return left.IsDir()
		}
		return left.Name() < right.Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, strings.Repeat("  ", depth)+name)
		if !entry.IsDir() || depth >= 1 {
			continue
		}
		children, err := os.ReadDir(filepath.Join(root, entry.Name()))
		if err != nil || len(children) == 0 {
			continue
		}
		lines = append(lines, renderDirTree(filepath.Join(root, entry.Name()), children, depth+1))
	}
	return strings.Join(lines, "\n")
}

func StrReplaceHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	args := call.Arguments
	requestedPath, ok := args["path"].(string)
	if !ok || strings.TrimSpace(requestedPath) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required")
	}
	oldStr, ok := args["old_str"].(string)
	if !ok {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("old_str is required")
	}
	newStr, ok := args["new_str"].(string)
	if !ok {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("new_str is required")
	}
	replaceAll, _ := args["replace_all"].(bool)
	path := tools.ResolveVirtualPath(ctx, requestedPath)
	if err := tools.ValidateWritableToolPath(ctx, requestedPath, path); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("replace failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	content := string(data)
	if !strings.Contains(content, oldStr) {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("string to replace not found")
	}
	if !replaceAll && strings.Count(content, oldStr) != 1 {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("string to replace must appear exactly once")
	}
	if replaceAll {
		content = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		content = strings.Replace(content, oldStr, newStr, 1)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("replace failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "OK"}, nil
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

func LsTool() models.Tool {
	return models.Tool{
		Name:        "ls",
		Description: "List the contents of a directory.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path to list"},
			},
			"required": []any{"path"},
		},
		Handler: LsHandler,
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
				"path":       map[string]any{"type": "string", "description": "File path to read"},
				"limit":      map[string]any{"type": "number", "description": "Maximum bytes to read"},
				"start_line": map[string]any{"type": "integer", "description": "Optional starting line number (1-indexed, inclusive)"},
				"end_line":   map[string]any{"type": "integer", "description": "Optional ending line number (1-indexed, inclusive)"},
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

func StrReplaceTool() models.Tool {
	return models.Tool{
		Name:        "str_replace",
		Description: "Replace a string in a file.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string", "description": "File path to update"},
				"old_str":     map[string]any{"type": "string", "description": "Substring to replace"},
				"new_str":     map[string]any{"type": "string", "description": "Replacement string"},
				"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences instead of just the first"},
			},
			"required": []any{"path", "old_str", "new_str"},
		},
		Handler: StrReplaceHandler,
	}
}

// FileTools returns all file operation tools.
func FileTools() []models.Tool {
	return []models.Tool{
		LsTool(),
		ReadFileTool(),
		WriteFileTool(),
		StrReplaceTool(),
		GlobTool(),
	}
}
