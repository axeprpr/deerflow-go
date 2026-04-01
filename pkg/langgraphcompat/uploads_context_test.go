package langgraphcompat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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
	if err := os.WriteFile(filepath.Join(uploadDir, "report.md"), []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write markdown companion: %v", err)
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
						"filename":     "report.pdf",
						"size":         3.0,
						"path":         "/tmp/ignored/report.pdf",
						"virtual_path": "/mnt/user-data/uploads/report.pdf",
						"status":       "uploaded",
					},
				},
				"element": "task",
			},
		},
	}

	messages := s.convertToMessages(threadID, input, false)
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
	if strings.Contains(content, "\n- report.md (") {
		t.Fatalf("unexpected standalone markdown companion in %q", content)
	}
	if !strings.Contains(content, "/mnt/user-data/uploads/report.pdf") {
		t.Fatalf("expected virtual upload path in %q", content)
	}
	if !strings.Contains(content, "Markdown copy: /mnt/user-data/uploads/report.md") {
		t.Fatalf("expected markdown companion path in %q", content)
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
	multi := decodeMultiContent(messages[0].Metadata)
	if len(multi) < 2 {
		t.Fatalf("multi_content len=%d want>=2", len(multi))
	}
	if got := multi[0]["type"]; got != "text" {
		t.Fatalf("multi_content[0].type=%v want text", got)
	}
	if text, _ := multi[0]["text"].(string); !strings.Contains(text, "<uploaded_files>") {
		t.Fatalf("multi_content missing upload context: %#v", multi[0])
	}
}

func TestConvertToMessagesNormalizesUploadedFilePathAndSkipsMissingFiles(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		dataRoot: root,
	}

	threadID := "thread-upload-normalize"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write current upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.md"), []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write markdown companion: %v", err)
	}

	input := []any{
		map[string]any{
			"role":    "user",
			"content": "analyse upload",
			"additional_kwargs": map[string]any{
				"files": []any{
					map[string]any{
						"filename": "report.pdf",
						"size":     "2048",
						"path":     "/tmp/arbitrary/report.pdf",
					},
					map[string]any{
						"filename": "missing.pdf",
						"size":     99,
						"path":     "/tmp/arbitrary/missing.pdf",
					},
				},
			},
		},
	}

	messages := s.convertToMessages(threadID, input, false)
	if len(messages) != 1 {
		t.Fatalf("messages len=%d want=1", len(messages))
	}

	content := messages[0].Content
	if !strings.Contains(content, "/mnt/user-data/uploads/report.pdf") {
		t.Fatalf("expected normalized upload path in %q", content)
	}
	if !strings.Contains(content, "Markdown copy: /mnt/user-data/uploads/report.md") {
		t.Fatalf("expected markdown companion path in %q", content)
	}
	if strings.Contains(content, "/tmp/arbitrary/report.pdf") {
		t.Fatalf("unexpected unnormalized path in %q", content)
	}
	if strings.Contains(content, "missing.pdf") {
		t.Fatalf("unexpected missing file in %q", content)
	}
	if strings.Contains(content, "\n- report.md (") {
		t.Fatalf("unexpected standalone markdown companion in %q", content)
	}
	if !strings.Contains(content, "2.0 KB") {
		t.Fatalf("expected parsed size in %q", content)
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

func TestConvertToMessagesPreservesMultimodalContent(t *testing.T) {
	server := &Server{}
	messages := server.convertToMessages("thread-vision", []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "describe this image"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			},
		},
	}, false)
	if len(messages) != 1 {
		t.Fatalf("messages len=%d want=1", len(messages))
	}
	multi := decodeMultiContent(messages[0].Metadata)
	if len(multi) != 2 {
		t.Fatalf("multi_content len=%d want=2", len(multi))
	}
	if got := multi[1]["type"]; got != "image_url" {
		t.Fatalf("multi_content[1].type=%v want image_url", got)
	}
}

func TestConvertToMessagesIncludesUploadedImagesForVisionModels(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		dataRoot: root,
	}

	threadID := "thread-upload-image"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	imageBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(uploadDir, "diagram.png"), imageBytes, 0o644); err != nil {
		t.Fatalf("write uploaded image: %v", err)
	}

	input := []any{
		map[string]any{
			"role":    "user",
			"content": "what is in this image?",
			"additional_kwargs": map[string]any{
				"files": []any{
					map[string]any{
						"filename": "diagram.png",
						"size":     len(imageBytes),
						"path":     "/mnt/user-data/uploads/diagram.png",
					},
				},
			},
		},
	}

	messages := s.convertToMessages(threadID, input, true)
	if len(messages) != 1 {
		t.Fatalf("messages len=%d want=1", len(messages))
	}

	multi := decodeMultiContent(messages[0].Metadata)
	if len(multi) != 2 {
		t.Fatalf("multi_content len=%d want=2", len(multi))
	}
	if got := multi[1]["type"]; got != "image_url" {
		t.Fatalf("multi_content[1].type=%v want image_url", got)
	}
	imageURL, _ := multi[1]["image_url"].(map[string]any)
	if got := imageURL["url"]; got != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("image_url=%v want data URL", got)
	}
}

func TestConvertToMessagesIncludesHistoricalUploadedImagesForVisionModels(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		dataRoot: root,
	}

	threadID := "thread-upload-image-history"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	imageBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(uploadDir, "old-diagram.png"), imageBytes, 0o644); err != nil {
		t.Fatalf("write historical uploaded image: %v", err)
	}

	input := []any{
		map[string]any{
			"role":    "user",
			"content": "what did I upload earlier?",
		},
	}

	messages := s.convertToMessages(threadID, input, true)
	if len(messages) != 1 {
		t.Fatalf("messages len=%d want=1", len(messages))
	}

	multi := decodeMultiContent(messages[0].Metadata)
	if len(multi) != 2 {
		t.Fatalf("multi_content len=%d want=2", len(multi))
	}
	if got := multi[1]["type"]; got != "image_url" {
		t.Fatalf("multi_content[1].type=%v want image_url", got)
	}
	imageURL, _ := multi[1]["image_url"].(map[string]any)
	if got := imageURL["url"]; got != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("image_url=%v want data URL", got)
	}
}

func TestConvertToMessagesCapsHistoricalUploadedImagesForVisionModels(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		dataRoot: root,
	}

	threadID := "thread-upload-image-cap"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	imageBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	for i := 0; i < maxUploadedImageParts+2; i++ {
		name := filepath.Join(uploadDir, "diagram-"+strconv.Itoa(i)+".png")
		if err := os.WriteFile(name, imageBytes, 0o644); err != nil {
			t.Fatalf("write uploaded image %d: %v", i, err)
		}
	}

	input := []any{
		map[string]any{
			"role":    "user",
			"content": "summarize my uploaded images",
		},
	}

	messages := s.convertToMessages(threadID, input, true)
	if len(messages) != 1 {
		t.Fatalf("messages len=%d want=1", len(messages))
	}

	multi := decodeMultiContent(messages[0].Metadata)
	if got := len(multi); got != 1+maxUploadedImageParts {
		t.Fatalf("multi_content len=%d want=%d", got, 1+maxUploadedImageParts)
	}
}

func TestConvertToMessagesSkipsUploadedImagesForNonVisionModels(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		dataRoot: root,
	}

	threadID := "thread-upload-image-disabled"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "diagram.png"), []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatalf("write uploaded image: %v", err)
	}

	input := []any{
		map[string]any{
			"role":    "user",
			"content": "what is in this image?",
			"additional_kwargs": map[string]any{
				"files": []any{
					map[string]any{
						"filename": "diagram.png",
						"size":     4,
						"path":     "/mnt/user-data/uploads/diagram.png",
					},
				},
			},
		},
	}

	messages := s.convertToMessages(threadID, input, false)
	if len(messages) != 1 {
		t.Fatalf("messages len=%d want=1", len(messages))
	}

	multi := decodeMultiContent(messages[0].Metadata)
	if len(multi) != 1 {
		t.Fatalf("multi_content len=%d want=1", len(multi))
	}
	if got := multi[0]["type"]; got != "text" {
		t.Fatalf("multi_content[0].type=%v want text", got)
	}
}

func TestMessagesToLangChainReturnsMultiContentWhenAvailable(t *testing.T) {
	server := &Server{}
	out := server.messagesToLangChain([]models.Message{
		{
			ID:        "m1",
			SessionID: "thread-1",
			Role:      models.RoleHuman,
			Content:   "describe this image",
			Metadata: map[string]string{
				"multi_content": `[{"type":"text","text":"describe this image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]`,
			},
		},
	})
	if len(out) != 1 {
		t.Fatalf("messages len=%d want=1", len(out))
	}
	parts, ok := out[0].Content.([]map[string]any)
	if !ok {
		t.Fatalf("content type=%T want []map[string]any", out[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("content len=%d want=2", len(parts))
	}
	if got := parts[1]["type"]; got != "image_url" {
		t.Fatalf("content[1].type=%v want image_url", got)
	}
}
