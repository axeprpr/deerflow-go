package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestReadFileHandlerResolvesThreadVirtualPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-file-tool"
	target := filepath.Join(root, "threads", threadID, "user-data", "uploads", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := ReadFileHandler(ctx, models.ToolCall{
		ID:   "call-1",
		Name: "read_file",
		Arguments: map[string]any{
			"path": "/mnt/user-data/uploads/notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("ReadFileHandler() error = %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("content=%q want hello", result.Content)
	}
}

func TestWriteFileHandlerWritesToResolvedVirtualPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-write-tool"
	ctx := tools.WithThreadID(context.Background(), threadID)
	_, err := WriteFileHandler(ctx, models.ToolCall{
		ID:   "call-2",
		Name: "write_file",
		Arguments: map[string]any{
			"path":    "/mnt/user-data/uploads/out.txt",
			"content": "created",
		},
	})
	if err != nil {
		t.Fatalf("WriteFileHandler() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "threads", threadID, "user-data", "uploads", "out.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "created" {
		t.Fatalf("content=%q want created", string(data))
	}
}

func TestGlobHandlerResolvesVirtualPattern(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-glob-tool"
	dir := filepath.Join(root, "threads", threadID, "user-data", "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := GlobHandler(ctx, models.ToolCall{
		ID:   "call-3",
		Name: "glob",
		Arguments: map[string]any{
			"pattern": "/mnt/user-data/uploads/*.txt",
		},
	})
	if err != nil {
		t.Fatalf("GlobHandler() error = %v", err)
	}
	if !strings.Contains(result.Content, "a.txt") || !strings.Contains(result.Content, "b.txt") {
		t.Fatalf("glob result=%q", result.Content)
	}
}

func TestReadFileHandlerResolvesACPWorkspaceVirtualPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-acp-read-tool"
	target := filepath.Join(root, "threads", threadID, "acp-workspace", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("from acp"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := ReadFileHandler(ctx, models.ToolCall{
		ID:   "call-acp-read-1",
		Name: "read_file",
		Arguments: map[string]any{
			"path": "/mnt/acp-workspace/notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("ReadFileHandler() error = %v", err)
	}
	if result.Content != "from acp" {
		t.Fatalf("content=%q want %q", result.Content, "from acp")
	}
}

func TestReadFileHandlerResolvesRelativePathToThreadWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-relative-read"
	target := filepath.Join(root, "threads", threadID, "user-data", "workspace", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("workspace note"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := ReadFileHandler(ctx, models.ToolCall{
		ID:   "call-relative-read-1",
		Name: "read_file",
		Arguments: map[string]any{
			"path": "notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("ReadFileHandler() error = %v", err)
	}
	if result.Content != "workspace note" {
		t.Fatalf("content=%q want %q", result.Content, "workspace note")
	}
}

func TestWriteFileHandlerWritesRelativePathToThreadWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-relative-write"
	ctx := tools.WithThreadID(context.Background(), threadID)
	_, err := WriteFileHandler(ctx, models.ToolCall{
		ID:   "call-relative-write-1",
		Name: "write_file",
		Arguments: map[string]any{
			"path":    "draft.txt",
			"content": "created in workspace",
		},
	})
	if err != nil {
		t.Fatalf("WriteFileHandler() error = %v", err)
	}

	target := filepath.Join(root, "threads", threadID, "user-data", "workspace", "draft.txt")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "created in workspace" {
		t.Fatalf("content=%q want %q", string(data), "created in workspace")
	}
}

func TestWriteFileHandlerAppendMode(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-append-write"
	ctx := tools.WithThreadID(context.Background(), threadID)
	for idx, content := range []string{"hello", " world"} {
		_, err := WriteFileHandler(ctx, models.ToolCall{
			ID:   "call-append-" + string(rune('1'+idx)),
			Name: "write_file",
			Arguments: map[string]any{
				"path":    "append.txt",
				"content": content,
				"append":  idx > 0,
			},
		})
		if err != nil {
			t.Fatalf("WriteFileHandler() error = %v", err)
		}
	}

	target := filepath.Join(root, "threads", threadID, "user-data", "workspace", "append.txt")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("content=%q want %q", string(data), "hello world")
	}
}

func TestGlobHandlerResolvesRelativePatternToThreadWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-relative-glob"
	dir := filepath.Join(root, "threads", threadID, "user-data", "workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := GlobHandler(ctx, models.ToolCall{
		ID:   "call-relative-glob-1",
		Name: "glob",
		Arguments: map[string]any{
			"pattern": "*.txt",
		},
	})
	if err != nil {
		t.Fatalf("GlobHandler() error = %v", err)
	}
	if !strings.Contains(result.Content, "a.txt") || !strings.Contains(result.Content, "b.txt") {
		t.Fatalf("glob result=%q", result.Content)
	}
}
