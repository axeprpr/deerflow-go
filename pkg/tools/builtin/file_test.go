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

func TestReadFileHandlerPrefersMarkdownCompanionForConvertibleUploads(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-convertible-upload"
	uploadDir := filepath.Join(root, "threads", threadID, "user-data", "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.pdf"), []byte("%PDF-binary"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.md"), []byte("# Report\n\nConverted content"), 0o644); err != nil {
		t.Fatalf("write markdown companion: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := ReadFileHandler(ctx, models.ToolCall{
		ID:   "call-read-convertible-1",
		Name: "read_file",
		Arguments: map[string]any{
			"path": "/mnt/user-data/uploads/report.pdf",
		},
	})
	if err != nil {
		t.Fatalf("ReadFileHandler() error = %v", err)
	}
	if result.Content != "# Report\n\nConverted content" {
		t.Fatalf("content=%q want markdown companion", result.Content)
	}
}

func TestReadFileHandlerPrefersMarkdownCompanionForStructuredUploads(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-structured-upload"
	uploadDir := filepath.Join(root, "threads", threadID, "user-data", "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "dataset.csv"), []byte("name,score\nalice,10\n"), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "dataset.md"), []byte("# Dataset\n\n| name | score |\n| --- | --- |\n| alice | 10 |"), 0o644); err != nil {
		t.Fatalf("write markdown companion: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := ReadFileHandler(ctx, models.ToolCall{
		ID:   "call-read-structured-1",
		Name: "read_file",
		Arguments: map[string]any{
			"path": "/mnt/user-data/uploads/dataset.csv",
		},
	})
	if err != nil {
		t.Fatalf("ReadFileHandler() error = %v", err)
	}
	if result.Content != "# Dataset\n\n| name | score |\n| --- | --- |\n| alice | 10 |" {
		t.Fatalf("content=%q want markdown companion", result.Content)
	}
}

func TestReadFileHandlerFallsBackToOriginalUploadWhenMarkdownCompanionMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-convertible-upload-fallback"
	uploadDir := filepath.Join(root, "threads", threadID, "user-data", "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.pdf"), []byte("raw-content"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := ReadFileHandler(ctx, models.ToolCall{
		ID:   "call-read-convertible-2",
		Name: "read_file",
		Arguments: map[string]any{
			"path": "/mnt/user-data/uploads/report.pdf",
		},
	})
	if err != nil {
		t.Fatalf("ReadFileHandler() error = %v", err)
	}
	if result.Content != "raw-content" {
		t.Fatalf("content=%q want raw-content", result.Content)
	}
}

func TestWriteFileHandlerWritesToResolvedVirtualPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-write-tool"
	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := WriteFileHandler(ctx, models.ToolCall{
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
	if result.Content != "OK" {
		t.Fatalf("content=%q want OK", result.Content)
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
	if !strings.Contains(result.Content, "/mnt/user-data/uploads/a.txt") || !strings.Contains(result.Content, "/mnt/user-data/uploads/b.txt") {
		t.Fatalf("glob result=%q", result.Content)
	}
	if strings.Contains(result.Content, root) {
		t.Fatalf("glob result=%q should not expose host root %q", result.Content, root)
	}
}

func TestLsHandlerListsResolvedVirtualDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-ls-tool"
	dir := filepath.Join(root, "threads", threadID, "user-data", "uploads")
	if err := os.MkdirAll(filepath.Join(dir, "nested", "deep"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "child.txt"), []byte("child"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "deep", "ignored.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write deep file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := LsHandler(ctx, models.ToolCall{
		ID:   "call-ls-1",
		Name: "ls",
		Arguments: map[string]any{
			"path": "/mnt/user-data/uploads",
		},
	})
	if err != nil {
		t.Fatalf("LsHandler() error = %v", err)
	}
	if !strings.Contains(result.Content, "a.txt") || !strings.Contains(result.Content, "nested/") {
		t.Fatalf("ls result=%q", result.Content)
	}
	if !strings.Contains(result.Content, "  child.txt") {
		t.Fatalf("ls result=%q missing nested child", result.Content)
	}
	if strings.Contains(result.Content, "ignored.txt") {
		t.Fatalf("ls result=%q should not include entries deeper than two levels", result.Content)
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

func TestWriteFileHandlerBlocksSkillsWrites(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_SKILLS_ROOT", root)

	skillPath := filepath.Join(root, "public", "bootstrap", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), "thread-skills-write-blocked")
	_, err := WriteFileHandler(ctx, models.ToolCall{
		ID:   "call-skills-write-1",
		Name: "write_file",
		Arguments: map[string]any{
			"path":    "/mnt/skills/public/bootstrap/SKILL.md",
			"content": "mutated",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Write access to skills path is not allowed") {
		t.Fatalf("error=%v want skills write denied", err)
	}

	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("content=%q want %q", string(data), "original")
	}
}

func TestStrReplaceHandlerBlocksACPWorkspaceWrites(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-acp-replace-blocked"
	target := filepath.Join(root, "threads", threadID, "acp-workspace", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("from acp"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	_, err := StrReplaceHandler(ctx, models.ToolCall{
		ID:   "call-acp-replace-1",
		Name: "str_replace",
		Arguments: map[string]any{
			"path":    "/mnt/acp-workspace/notes.txt",
			"old_str": "acp",
			"new_str": "agent",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Write access to ACP workspace is not allowed") {
		t.Fatalf("error=%v want acp write denied", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "from acp" {
		t.Fatalf("content=%q want %q", string(data), "from acp")
	}
}

func TestReadFileHandlerSupportsLineRanges(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-read-range"
	target := filepath.Join(root, "threads", threadID, "user-data", "workspace", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := ReadFileHandler(ctx, models.ToolCall{
		ID:   "call-read-range-1",
		Name: "read_file",
		Arguments: map[string]any{
			"path":       "notes.txt",
			"start_line": 2.0,
			"end_line":   3.0,
		},
	})
	if err != nil {
		t.Fatalf("ReadFileHandler() error = %v", err)
	}
	if result.Content != "two\nthree" {
		t.Fatalf("content=%q want %q", result.Content, "two\nthree")
	}
}

func TestStrReplaceHandlerUpdatesResolvedFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-str-replace"
	target := filepath.Join(root, "threads", threadID, "user-data", "workspace", "draft.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := StrReplaceHandler(ctx, models.ToolCall{
		ID:   "call-replace-1",
		Name: "str_replace",
		Arguments: map[string]any{
			"path":    "draft.txt",
			"old_str": "world",
			"new_str": "deerflow",
		},
	})
	if err != nil {
		t.Fatalf("StrReplaceHandler() error = %v", err)
	}
	if result.Content != "OK" {
		t.Fatalf("content=%q want OK", result.Content)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "hello deerflow" {
		t.Fatalf("content=%q want %q", string(data), "hello deerflow")
	}
}

func TestStrReplaceHandlerRequiresSingleMatchUnlessReplaceAll(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-str-replace-multi"
	target := filepath.Join(root, "threads", threadID, "user-data", "workspace", "draft.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("world world"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	_, err := StrReplaceHandler(ctx, models.ToolCall{
		ID:   "call-replace-multi-1",
		Name: "str_replace",
		Arguments: map[string]any{
			"path":    "draft.txt",
			"old_str": "world",
			"new_str": "deerflow",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "exactly once") {
		t.Fatalf("error=%v want exactly once failure", err)
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
	if !strings.Contains(result.Content, "/mnt/user-data/workspace/a.txt") || !strings.Contains(result.Content, "/mnt/user-data/workspace/b.txt") {
		t.Fatalf("glob result=%q", result.Content)
	}
	if strings.Contains(result.Content, root) {
		t.Fatalf("glob result=%q should not expose host root %q", result.Content, root)
	}
}

func TestReadFileHandlerMasksHostPathInErrors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-read-mask-error"
	ctx := tools.WithThreadID(context.Background(), threadID)
	_, err := ReadFileHandler(ctx, models.ToolCall{
		ID:   "call-read-mask-error-1",
		Name: "read_file",
		Arguments: map[string]any{
			"path": "/mnt/user-data/workspace/missing.txt",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("error=%q should not expose host root %q", err.Error(), root)
	}
	if !strings.Contains(err.Error(), "/mnt/user-data/workspace/missing.txt") {
		t.Fatalf("error=%q missing virtual path", err.Error())
	}
}
