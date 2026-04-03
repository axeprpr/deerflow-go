package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/tools"
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

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/thread-state-post/state", strings.NewReader(`{"values":{"title":"Updated title","view_mode":"canvas"},"metadata":{"agent_type":"coder"}}`), map[string]string{
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
	if got := asString(session.Values["view_mode"]); got != "canvas" {
		t.Fatalf("view_mode=%q want=canvas", got)
	}
	if got := asString(session.Metadata["agent_type"]); got != "coder" {
		t.Fatalf("agent_type=%q want=coder", got)
	}
}

func TestThreadStatePatchPersistsCustomValuesAcrossReload(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-custom-values", map[string]any{"title": "Custom values"})

	resp := performCompatRequest(t, handler, http.MethodPatch, "/threads/thread-custom-values/state", strings.NewReader(`{"values":{"sidebar_tab":"artifacts","draft_id":"draft-42"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	reloaded := &Server{
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
		runStreams: make(map[string]map[uint64]chan StreamEvent),
		dataRoot:   s.dataRoot,
	}
	if err := reloaded.loadPersistedSessions(); err != nil {
		t.Fatalf("loadPersistedSessions() error = %v", err)
	}

	state := reloaded.getThreadState("thread-custom-values")
	if state == nil {
		t.Fatal("state is nil")
	}
	if got := asString(state.Values["sidebar_tab"]); got != "artifacts" {
		t.Fatalf("sidebar_tab=%q want=artifacts", got)
	}
	if got := asString(state.Values["draft_id"]); got != "draft-42" {
		t.Fatalf("draft_id=%q want=draft-42", got)
	}
}

func TestThreadHistoryPreservesCustomValues(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-history-custom-values", map[string]any{"title": "History values"})

	resp := performCompatRequest(t, handler, http.MethodPatch, "/threads/thread-history-custom-values/state", strings.NewReader(`{"values":{"sidebar_tab":"artifacts","draft_id":"draft-42"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	history := s.threadHistory("thread-history-custom-values")
	if len(history) == 0 {
		t.Fatal("history is empty")
	}
	if got := asString(history[0].Values["sidebar_tab"]); got != "artifacts" {
		t.Fatalf("sidebar_tab=%q want=artifacts", got)
	}
	if got := asString(history[0].Values["draft_id"]); got != "draft-42" {
		t.Fatalf("draft_id=%q want=draft-42", got)
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

	sandbox, ok := state.Values["sandbox"].(map[string]any)
	if !ok {
		t.Fatalf("sandbox=%#v", state.Values["sandbox"])
	}
	if got := asString(sandbox["sandbox_id"]); got != "local" {
		t.Fatalf("sandbox_id=%q want=local", got)
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

func TestThreadFilesIncludeAutoDiscoveredArtifacts(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-files-autodiscovered"
	session := s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	outputPath := filepath.Join(outputDir, "report.md")
	if err := os.WriteFile(outputPath, []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	workspaceDir := filepath.Join(s.threadRoot(threadID), "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "notes.txt"), []byte("draft"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	baseTime := time.Date(2026, 4, 3, 9, 0, 0, 0, time.UTC)
	if err := os.Chtimes(outputPath, baseTime, baseTime); err != nil {
		t.Fatalf("chtimes output: %v", err)
	}
	workspacePath := filepath.Join(workspaceDir, "notes.txt")
	workspaceTime := baseTime.Add(2 * time.Minute)
	if err := os.Chtimes(workspacePath, workspaceTime, workspaceTime); err != nil {
		t.Fatalf("chtimes workspace: %v", err)
	}
	uploadPath := filepath.Join(uploadDir, "brief.pdf")
	uploadTime := baseTime.Add(4 * time.Minute)
	if err := os.Chtimes(uploadPath, uploadTime, uploadTime); err != nil {
		t.Fatalf("chtimes upload: %v", err)
	}

	if err := session.PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/report.md",
		SourcePath: outputPath,
		CreatedAt:  baseTime,
	}); err != nil {
		t.Fatalf("register presented file: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads/"+threadID+"/files", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Files []tools.PresentFile `json:"files"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.Files) != 3 {
		t.Fatalf("files=%d want=3 payload=%#v", len(payload.Files), payload.Files)
	}

	got := []string{payload.Files[0].Path, payload.Files[1].Path, payload.Files[2].Path}
	want := []string{
		"/mnt/user-data/uploads/brief.pdf",
		"/mnt/user-data/workspace/notes.txt",
		"/mnt/user-data/outputs/report.md",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("paths=%q want=%q", strings.Join(got, ","), strings.Join(want, ","))
	}
	if payload.Files[0].MimeType != "application/pdf" {
		t.Fatalf("upload mime=%q want application/pdf", payload.Files[0].MimeType)
	}
	if payload.Files[1].MimeType != "text/plain; charset=utf-8" {
		t.Fatalf("workspace mime=%q want text/plain; charset=utf-8", payload.Files[1].MimeType)
	}
	if payload.Files[2].MimeType != "text/markdown; charset=utf-8" {
		t.Fatalf("output mime=%q want text/markdown; charset=utf-8", payload.Files[2].MimeType)
	}
	for i, file := range payload.Files {
		if strings.TrimSpace(file.ID) == "" {
			t.Fatalf("files[%d] missing id: %#v", i, file)
		}
		if file.CreatedAt.IsZero() {
			t.Fatalf("files[%d] missing created_at: %#v", i, file)
		}
	}
}

func TestThreadFilesSortAcrossRootsByNewestModifiedTime(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-files-global-sort"
	session := s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	outputPath := filepath.Join(outputDir, "report.md")
	if err := os.WriteFile(outputPath, []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	workspaceDir := filepath.Join(s.threadRoot(threadID), "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	workspacePath := filepath.Join(workspaceDir, "notes.txt")
	if err := os.WriteFile(workspacePath, []byte("draft"), 0o644); err != nil {
		t.Fatalf("write workspace: %v", err)
	}

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	uploadPath := filepath.Join(uploadDir, "brief.pdf")
	if err := os.WriteFile(uploadPath, []byte("%PDF"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	baseTime := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(outputPath, baseTime, baseTime); err != nil {
		t.Fatalf("chtimes output: %v", err)
	}
	workspaceTime := baseTime.Add(3 * time.Minute)
	if err := os.Chtimes(workspacePath, workspaceTime, workspaceTime); err != nil {
		t.Fatalf("chtimes workspace: %v", err)
	}
	uploadTime := baseTime.Add(6 * time.Minute)
	if err := os.Chtimes(uploadPath, uploadTime, uploadTime); err != nil {
		t.Fatalf("chtimes upload: %v", err)
	}

	if err := session.PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/report.md",
		SourcePath: outputPath,
		CreatedAt:  baseTime,
	}); err != nil {
		t.Fatalf("register presented file: %v", err)
	}

	files := s.sessionFiles(session)
	if len(files) != 3 {
		t.Fatalf("files=%d want=3", len(files))
	}

	got := []string{files[0].Path, files[1].Path, files[2].Path}
	want := []string{
		"/mnt/user-data/uploads/brief.pdf",
		"/mnt/user-data/workspace/notes.txt",
		"/mnt/user-data/outputs/report.md",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("paths=%q want=%q", strings.Join(got, ","), strings.Join(want, ","))
	}
}

func TestThreadFilesSkipDeletedPresentedArtifacts(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-files-skip-deleted-presented"
	session := s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	livePath := filepath.Join(outputDir, "report.md")
	if err := os.WriteFile(livePath, []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write live output: %v", err)
	}
	deletedPath := filepath.Join(outputDir, "deleted.md")
	if err := os.WriteFile(deletedPath, []byte("# Deleted"), 0o644); err != nil {
		t.Fatalf("write deleted output: %v", err)
	}

	for _, file := range []tools.PresentFile{
		{Path: "/mnt/user-data/outputs/report.md", SourcePath: livePath},
		{Path: "/mnt/user-data/outputs/deleted.md", SourcePath: deletedPath},
	} {
		if err := session.PresentFiles.Register(file); err != nil {
			t.Fatalf("register presented file %s: %v", file.Path, err)
		}
	}
	if err := os.Remove(deletedPath); err != nil {
		t.Fatalf("remove deleted output: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads/"+threadID+"/files", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Files []tools.PresentFile `json:"files"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.Files) != 1 {
		t.Fatalf("files=%d want=1 payload=%#v", len(payload.Files), payload.Files)
	}
	if got := payload.Files[0].Path; got != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("path=%q want=/mnt/user-data/outputs/report.md", got)
	}
}

func TestThreadStateFallsBackToMetadataAgentName(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.ensureSession("thread-agent-name", map[string]any{
		"title":      "Agent thread",
		"agent_name": "code-reviewer",
	})

	state := s.getThreadState("thread-agent-name")
	if state == nil {
		t.Fatal("state is nil")
	}

	config, ok := state.Config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", state.Config)
	}
	if got := asString(config["agent_name"]); got != "code-reviewer" {
		t.Fatalf("agent_name=%q want=code-reviewer", got)
	}
}

func TestThreadStateCheckpointIDStableUntilStateChanges(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-checkpoint", map[string]any{"title": "Checkpoint thread"})

	first := performCompatRequest(t, handler, http.MethodGet, "/threads/thread-checkpoint/state", nil, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := performCompatRequest(t, handler, http.MethodGet, "/threads/thread-checkpoint/state", nil, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}

	var firstState ThreadState
	if err := json.Unmarshal(first.Body.Bytes(), &firstState); err != nil {
		t.Fatalf("unmarshal first state: %v", err)
	}
	var secondState ThreadState
	if err := json.Unmarshal(second.Body.Bytes(), &secondState); err != nil {
		t.Fatalf("unmarshal second state: %v", err)
	}
	if firstState.CheckpointID == "" {
		t.Fatal("first checkpoint_id is empty")
	}
	if secondState.CheckpointID != firstState.CheckpointID {
		t.Fatalf("checkpoint_id changed across reads: %q != %q", secondState.CheckpointID, firstState.CheckpointID)
	}

	patch := performCompatRequest(t, handler, http.MethodPatch, "/threads/thread-checkpoint/state", strings.NewReader(`{"values":{"sidebar_tab":"artifacts"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if patch.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patch.Code, patch.Body.String())
	}

	var patchedState ThreadState
	if err := json.Unmarshal(patch.Body.Bytes(), &patchedState); err != nil {
		t.Fatalf("unmarshal patched state: %v", err)
	}
	if patchedState.CheckpointID == "" {
		t.Fatal("patched checkpoint_id is empty")
	}
	if patchedState.CheckpointID == firstState.CheckpointID {
		t.Fatalf("checkpoint_id did not change after state update: %q", patchedState.CheckpointID)
	}
}

func TestReloadedSessionKeepsLatestCheckpointID(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-reload-checkpoint", map[string]any{"title": "Reload checkpoint"})

	resp := performCompatRequest(t, handler, http.MethodPatch, "/threads/thread-reload-checkpoint/state", strings.NewReader(`{"values":{"draft_id":"draft-42"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.Code, resp.Body.String())
	}

	var patchedState ThreadState
	if err := json.Unmarshal(resp.Body.Bytes(), &patchedState); err != nil {
		t.Fatalf("unmarshal patched state: %v", err)
	}
	if patchedState.CheckpointID == "" {
		t.Fatal("patched checkpoint_id is empty")
	}

	reloaded := &Server{
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
		runStreams: make(map[string]map[uint64]chan StreamEvent),
		dataRoot:   s.dataRoot,
	}
	if err := reloaded.loadPersistedSessions(); err != nil {
		t.Fatalf("loadPersistedSessions() error = %v", err)
	}

	state := reloaded.getThreadState("thread-reload-checkpoint")
	if state == nil {
		t.Fatal("state is nil after reload")
	}
	if state.CheckpointID != patchedState.CheckpointID {
		t.Fatalf("checkpoint_id=%q want=%q", state.CheckpointID, patchedState.CheckpointID)
	}

	history := reloaded.threadHistory("thread-reload-checkpoint")
	if len(history) == 0 {
		t.Fatal("history is empty after reload")
	}
	if history[0].CheckpointID != patchedState.CheckpointID {
		t.Fatalf("history checkpoint_id=%q want=%q", history[0].CheckpointID, patchedState.CheckpointID)
	}
}

func TestThreadResponseFallsBackToMetadataAgentName(t *testing.T) {
	s, _ := newCompatTestServer(t)
	session := s.ensureSession("thread-agent-response", map[string]any{
		"title":      "Agent thread",
		"agent_name": "writer-bot",
	})

	resp := s.threadResponse(session)
	config, ok := resp["config"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", resp["config"])
	}
	configurable, ok := config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("configurable=%#v", config["configurable"])
	}
	if got := asString(configurable["agent_name"]); got != "writer-bot" {
		t.Fatalf("agent_name=%q want=writer-bot", got)
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

	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	wantArtifacts := []string{"/mnt/user-data/uploads/report.md"}
	if strings.Join(artifacts, ",") != strings.Join(wantArtifacts, ",") {
		t.Fatalf("artifacts=%#v want=%#v", artifacts, wantArtifacts)
	}
}
