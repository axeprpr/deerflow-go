package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestBashHandlerResolvesThreadVirtualPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-bash-tool"
	ctx := tools.WithThreadID(context.Background(), threadID)

	result, err := BashHandler(ctx, models.ToolCall{
		ID:   "call-bash-1",
		Name: "bash",
		Arguments: map[string]any{
			"command": "mkdir -p /mnt/user-data/workspace /mnt/user-data/outputs && printf 'draft' > /mnt/user-data/workspace/note.txt && cp /mnt/user-data/workspace/note.txt /mnt/user-data/outputs/result.txt && cat /mnt/user-data/outputs/result.txt",
		},
	})
	if err != nil {
		t.Fatalf("BashHandler() error = %v", err)
	}

	var output BashOutput
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if output.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", output.ExitCode, output.Stdout, output.Stderr)
	}
	if got := strings.TrimSpace(output.Stdout); got != "draft" {
		t.Fatalf("stdout = %q, want draft", got)
	}

	target := filepath.Join(root, "threads", threadID, "user-data", "outputs", "result.txt")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if string(data) != "draft" {
		t.Fatalf("file content = %q, want draft", string(data))
	}
}

func TestResolveVirtualCommandWithoutThreadIDLeavesCommandUntouched(t *testing.T) {
	cmd := "cat /mnt/user-data/uploads/demo.txt"
	if got := tools.ResolveVirtualCommand(context.Background(), cmd); got != cmd {
		t.Fatalf("command = %q, want %q", got, cmd)
	}
}

func TestBashHandlerResolvesACPWorkspacePaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	threadID := "thread-bash-acp"
	acpDir := filepath.Join(root, "threads", threadID, "acp-workspace")
	if err := os.MkdirAll(acpDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(acpDir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := tools.WithThreadID(context.Background(), threadID)
	result, err := BashHandler(ctx, models.ToolCall{
		ID:   "call-bash-acp-1",
		Name: "bash",
		Arguments: map[string]any{
			"command": "cat /mnt/acp-workspace/hello.txt",
		},
	})
	if err != nil {
		t.Fatalf("BashHandler() error = %v", err)
	}

	var output BashOutput
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got := strings.TrimSpace(output.Stdout); got != "hello" {
		t.Fatalf("stdout=%q want %q", got, "hello")
	}
}
