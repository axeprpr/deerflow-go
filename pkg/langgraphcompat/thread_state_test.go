package langgraphcompat

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThreadStatePatchUpdatesTitleFromValues(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-rename", map[string]any{"title": "Old title"})

	resp := performCompatRequest(t, handler, http.MethodPatch, "/threads/thread-rename/state", strings.NewReader(`{"values":{"title":"New title"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	state := s.getThreadState("thread-rename")
	if state == nil {
		t.Fatal("state is nil")
	}
	if got := asString(state.Values["title"]); got != "New title" {
		t.Fatalf("title=%q want=New title", got)
	}
}

func TestThreadStatePostMergesValuesAndMetadata(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-state-post", map[string]any{"title": "Old title", "agent_type": "writer"})

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/thread-state-post/state", strings.NewReader(`{"values":{"title":"Updated title"},"metadata":{"agent_type":"coder"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	s.sessionsMu.RLock()
	session := s.sessions["thread-state-post"]
	s.sessionsMu.RUnlock()
	if session == nil {
		t.Fatal("session is nil")
	}
	if got := asString(session.Metadata["title"]); got != "Updated title" {
		t.Fatalf("title=%q want=Updated title", got)
	}
	if got := asString(session.Metadata["agent_type"]); got != "coder" {
		t.Fatalf("agent_type=%q want=coder", got)
	}
}

func TestThreadStateIncludesThreadDataAndConfigurableContext(t *testing.T) {
	s, _ := newCompatTestServer(t)
	session := s.ensureSession("thread-context", map[string]any{"title": "Context thread", "agent_type": "coder"})
	session.Configurable["model_name"] = "gpt-5"
	session.Configurable["is_plan_mode"] = true
	session.Configurable["reasoning_effort"] = "high"

	state := s.getThreadState("thread-context")
	if state == nil {
		t.Fatal("state is nil")
	}

	threadData, ok := state.Values["thread_data"].(map[string]any)
	if !ok {
		t.Fatalf("thread_data=%#v", state.Values["thread_data"])
	}
	if got := asString(threadData["workspace_path"]); !strings.Contains(got, "/threads/thread-context/user-data/workspace") {
		t.Fatalf("workspace_path=%q", got)
	}

	config, ok := state.Config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", state.Config)
	}
	if got := asString(config["model_name"]); got != "gpt-5" {
		t.Fatalf("model_name=%q want=gpt-5", got)
	}
	if got, _ := config["is_plan_mode"].(bool); !got {
		t.Fatalf("is_plan_mode=%v want=true", config["is_plan_mode"])
	}
	if got := asString(config["reasoning_effort"]); got != "high" {
		t.Fatalf("reasoning_effort=%q want=high", got)
	}
}

func TestThreadStateIncludesStructuredUploadedFiles(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-uploaded-files"
	s.ensureSession(threadID, map[string]any{"title": "Uploads"})

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.md"), []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write markdown companion: %v", err)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state is nil")
	}

	uploadedFiles, ok := state.Values["uploaded_files"].([]map[string]any)
	if !ok {
		t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
	}
	if len(uploadedFiles) != 1 {
		t.Fatalf("uploaded_files len=%d want=1", len(uploadedFiles))
	}
	if got := asString(uploadedFiles[0]["filename"]); got != "report.pdf" {
		t.Fatalf("filename=%q want=report.pdf", got)
	}
	if got := asString(uploadedFiles[0]["path"]); got != "/mnt/user-data/uploads/report.pdf" {
		t.Fatalf("path=%q want=/mnt/user-data/uploads/report.pdf", got)
	}
	if got := asString(uploadedFiles[0]["extension"]); got != ".pdf" {
		t.Fatalf("extension=%q want=.pdf", got)
	}
	if got := asString(uploadedFiles[0]["markdown_path"]); got != "/mnt/user-data/uploads/report.md" {
		t.Fatalf("markdown_path=%q want=/mnt/user-data/uploads/report.md", got)
	}
}
