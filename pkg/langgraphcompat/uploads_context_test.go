package langgraphcompat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func TestConvertToMessagesInjectsUploadedFilesBlockAndPreservesKwargs(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		dataRoot: root,
	}

	threadID := "thread-upload-context"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write historical upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write current upload: %v", err)
	}

	input := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "please analyse"},
			},
			"additional_kwargs": map[string]any{
				"files": []any{
					map[string]any{
						"filename":    "report.pdf",
						"size":       3.0,
						"path":       "/tmp/ignored/report.pdf",
						"virtual_path": "/mnt/user-data/uploads/report.pdf",
						"status":    "uploaded",
					},
				},
				"element": "task",
			},
		},
	}

	messages := s.convertToMessages(threadID, input)
	if len(messages) != 1 {
		t.Fatalf("messages len=%d want=1", len(messages))
	}

	content := messages[0].Content
	if !strings.Contains(content, "<uploaded_files>") {
		t.Fatalf("expected uploaded_files block in %q", content)
	}
	if !strings.Contains(content, "report.pdf") || !strings.Contains(content, "old.txt") {
		t.Fatalf("expected current and historical uploads in %q", content)
	}
	if !strings.Contains(content, "/mnt/user-data/uploads/report.pdf") {
		t.Fatalf("expected virtual upload path in %q", content)
	}
	if !strings.Contains(content, "please analyse") {
		t.Fatalf("expected original user content in %q", content)
	}

	kwargs := decodeAdditionalKwargs(messages[0].Metadata)
	if kwargs["element"] != "task" {
		t.Fatalf("element=%v want task", kwargs["element"])
	}
	files, _ := kwargs["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files len=%d want=1", len(files))
	}
}

func TestMessagesToLangChainReturnsAdditionalKwargs(t *testing.T) {
	additionalKwargs, err := json.Marshal(map[string]any{
		"files": []map[string]any{
			{"filename": "demo.txt", "size": 5, "path": "/mnt/user-data/uploads/demo.txt"},
		},
	})
	if err != nil {
		t.Fatalf("marshal additional kwargs: %v", err)
	}

	server := &Server{}
	out := server.messagesToLangChain([]models.Message{
		{
			ID:        "m1",
			SessionID: "thread-1",
			Role:      models.RoleHuman,
			Content:   "hello",
			Metadata:  map[string]string{"additional_kwargs": string(additionalKwargs)},
		},
	})
	if len(out) != 1 {
		t.Fatalf("messages len=%d want=1", len(out))
	}
	if out[0].AdditionalKwargs == nil {
		t.Fatal("expected additional_kwargs to be returned")
	}
	files, _ := out[0].AdditionalKwargs["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files len=%d want=1", len(files))
	}
}
