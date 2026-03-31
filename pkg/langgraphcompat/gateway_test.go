package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	toolctx "github.com/axeprpr/deerflow-go/pkg/tools"
)

func newCompatTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	root := t.TempDir()
	s := &Server{
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
		runStreams: make(map[string]map[uint64]chan StreamEvent),
		dataRoot:   root,
		startedAt:  time.Now().UTC(),
		skills:     defaultGatewaySkills(),
		mcpConfig:  defaultGatewayMCPConfig(),
		agents:     map[string]gatewayAgent{},
		memory:     defaultGatewayMemory(),
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return s, mux
}

func performCompatRequest(t *testing.T, handler http.Handler, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func waitForRunSubscriber(t *testing.T, s *Server, runID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.runsMu.RLock()
		count := len(s.runStreams[runID])
		s.runsMu.RUnlock()
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for run subscriber on %q", runID)
}

type fakeGatewayMCPClient struct {
	tools  []models.Tool
	closed bool
}

func (f *fakeGatewayMCPClient) Tools(ctx context.Context) ([]models.Tool, error) {
	return append([]models.Tool(nil), f.tools...), nil
}

func (f *fakeGatewayMCPClient) Close() error {
	f.closed = true
	return nil
}

func TestMessagesToLangChainPreservesToolCallsAndUsageMetadata(t *testing.T) {
	s, _ := newCompatTestServer(t)
	messages := []models.Message{
		{
			ID:        "ai-1",
			SessionID: "thread-1",
			Role:      models.RoleAI,
			Content:   "Working on it",
			ToolCalls: []models.ToolCall{{
				ID:        "call-1",
				Name:      "present_file",
				Arguments: map[string]any{"content": "artifact body"},
				Status:    models.CallStatusCompleted,
			}},
			Metadata: map[string]string{
				"usage_metadata": `{"input_tokens":11,"output_tokens":7,"total_tokens":18}`,
			},
		},
		{
			ID:        "tool-1",
			SessionID: "thread-1",
			Role:      models.RoleTool,
			Content:   "artifact body",
			ToolResult: &models.ToolResult{
				CallID:   "call-1",
				ToolName: "present_file",
				Status:   models.CallStatusCompleted,
				Content:  "artifact body",
			},
		},
	}

	got := s.messagesToLangChain(messages)
	if len(got) != 2 {
		t.Fatalf("messages=%d want=2", len(got))
	}
	if len(got[0].ToolCalls) != 1 || got[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("ai tool_calls=%#v", got[0].ToolCalls)
	}
	if got[0].UsageMetadata["total_tokens"] != 18 {
		t.Fatalf("usage_metadata=%#v", got[0].UsageMetadata)
	}
	if got[1].Name != "present_file" || got[1].ToolCallID != "call-1" {
		t.Fatalf("tool message name=%q tool_call_id=%q", got[1].Name, got[1].ToolCallID)
	}
}

func TestForwardAgentEventEmitsCompatibleMessagesTuplePayloads(t *testing.T) {
	s, _ := newCompatTestServer(t)
	rec := httptest.NewRecorder()
	run := &Run{RunID: "run-1", ThreadID: "thread-1"}

	s.forwardAgentEvent(rec, rec, run, agent.AgentEvent{
		Type:      agent.AgentEventChunk,
		MessageID: "ai-msg-1",
		Text:      "Hello",
	})
	s.forwardAgentEvent(rec, rec, run, agent.AgentEvent{
		Type:      agent.AgentEventToolCall,
		MessageID: "ai-msg-1",
		ToolCall: &models.ToolCall{
			ID:        "call-1",
			Name:      "present_file",
			Arguments: map[string]any{"content": "full artifact"},
			Status:    models.CallStatusPending,
		},
		ToolEvent: &agent.ToolCallEvent{
			ID:     "call-1",
			Name:   "present_file",
			Status: models.CallStatusPending,
		},
	})
	s.forwardAgentEvent(rec, rec, run, agent.AgentEvent{
		Type:      agent.AgentEventToolCallEnd,
		MessageID: "tool-msg-1",
		Result: &models.ToolResult{
			CallID:   "call-1",
			ToolName: "present_file",
			Status:   models.CallStatusCompleted,
			Content:  "full artifact",
		},
		ToolEvent: &agent.ToolCallEvent{
			ID:            "call-1",
			Name:          "present_file",
			Status:        models.CallStatusCompleted,
			ResultPreview: "truncated preview",
		},
	})

	body := rec.Body.String()
	if !strings.Contains(body, `event: messages-tuple`) {
		t.Fatalf("expected messages-tuple event in %q", body)
	}
	if !strings.Contains(body, `"id":"ai-msg-1"`) {
		t.Fatalf("expected ai message id in %q", body)
	}
	if !strings.Contains(body, `"tool_calls":[{"id":"call-1","name":"present_file","args":{"content":"full artifact"}}]`) {
		t.Fatalf("expected tool_calls payload in %q", body)
	}
	if !strings.Contains(body, `"id":"tool-msg-1"`) || !strings.Contains(body, `"content":"full artifact"`) {
		t.Fatalf("expected full tool result payload in %q", body)
	}
}

func TestThreadRunStreamContinuesWithLiveEvents(t *testing.T) {
	s, handler := newCompatTestServer(t)
	run := &Run{
		RunID:     "run-live",
		ThreadID:  "thread-live",
		Status:    "running",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Events: []StreamEvent{{
			ID:       "run-live:1",
			Event:    "metadata",
			Data:     map[string]any{"run_id": "run-live"},
			RunID:    "run-live",
			ThreadID: "thread-live",
		}},
	}
	s.saveRun(run)

	bodyCh := make(chan string, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/threads/thread-live/runs/run-live/stream", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		bodyCh <- rec.Body.String()
	}()

	waitForRunSubscriber(t, s, "run-live")
	s.appendRunEvent("run-live", StreamEvent{
		ID:       "run-live:2",
		Event:    "updates",
		Data:     map[string]any{"agent": map[string]any{"title": "Still running"}},
		RunID:    "run-live",
		ThreadID: "thread-live",
	})
	s.appendRunEvent("run-live", StreamEvent{
		ID:       "run-live:3",
		Event:    "end",
		Data:     map[string]any{"run_id": "run-live"},
		RunID:    "run-live",
		ThreadID: "thread-live",
	})

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, "event: metadata") {
			t.Fatalf("expected metadata event in %q", body)
		}
		if !strings.Contains(body, "event: updates") {
			t.Fatalf("expected updates event in %q", body)
		}
		if !strings.Contains(body, "event: end") {
			t.Fatalf("expected end event in %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for stream response")
	}
}

func TestThreadJoinStreamFollowsLatestRun(t *testing.T) {
	s, handler := newCompatTestServer(t)
	run := &Run{
		RunID:     "run-join",
		ThreadID:  "thread-join",
		Status:    "running",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	s.saveRun(run)

	bodyCh := make(chan string, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/threads/thread-join/stream", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		bodyCh <- rec.Body.String()
	}()

	waitForRunSubscriber(t, s, "run-join")
	s.appendRunEvent("run-join", StreamEvent{
		ID:       "run-join:1",
		Event:    "messages-tuple",
		Data:     Message{Type: "ai", ID: "msg-1", Role: "assistant", Content: "hello"},
		RunID:    "run-join",
		ThreadID: "thread-join",
	})
	s.appendRunEvent("run-join", StreamEvent{
		ID:       "run-join:2",
		Event:    "error",
		Data:     map[string]any{"message": "boom"},
		RunID:    "run-join",
		ThreadID: "thread-join",
	})

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, "event: messages-tuple") {
			t.Fatalf("expected messages-tuple event in %q", body)
		}
		if !strings.Contains(body, `"content":"hello"`) {
			t.Fatalf("expected live message payload in %q", body)
		}
		if !strings.Contains(body, "event: error") {
			t.Fatalf("expected error event in %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for join stream response")
	}
}

func TestAPILangGraphPrefixCreateThread(t *testing.T) {
	_, handler := newCompatTestServer(t)
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/langgraph/threads", strings.NewReader(`{}`), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestThreadHistorySupportsGETLimitQuery(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-history", map[string]any{"title": "Saved thread"})
	session.Messages = []models.Message{{
		ID:        "msg-1",
		SessionID: "thread-history",
		Role:      models.RoleHuman,
		Content:   "hello",
	}}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/langgraph/threads/thread-history/history?limit=1", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var history []ThreadState
	if err := json.Unmarshal(resp.Body.Bytes(), &history); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history len=%d want=1", len(history))
	}
	if got := asString(history[0].Values["title"]); got != "Saved thread" {
		t.Fatalf("title=%q want Saved thread", got)
	}
}

func TestThreadHistoryRejectsInvalidGETLimit(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-history", nil)

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads/thread-history/history?limit=abc", nil, nil)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestPersistedSessionsReloadMessagesTodosAndArtifacts(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-persisted"
	session := s.ensureSession(threadID, map[string]any{"title": "Saved thread"})
	session.Messages = []models.Message{{
		ID:        "msg-1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}
	session.Todos = []Todo{{Content: "Keep state", Status: "in_progress"}}
	session.UpdatedAt = time.Now().UTC()
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	artifactPath := filepath.Join(s.threadRoot(threadID), "outputs", "report.md")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("# report"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	reloaded := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		dataRoot: s.dataRoot,
	}
	if err := reloaded.loadPersistedSessions(); err != nil {
		t.Fatalf("load persisted sessions: %v", err)
	}

	state := reloaded.getThreadState(threadID)
	if state == nil {
		t.Fatal("state is nil")
	}
	messages, ok := state.Values["messages"].([]Message)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages=%#v", state.Values["messages"])
	}
	todos, ok := state.Values["todos"].([]map[string]any)
	if !ok || len(todos) != 1 {
		t.Fatalf("todos=%#v", state.Values["todos"])
	}
	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if artifacts[0] != "/mnt/user-data/outputs/report.md" {
		t.Fatalf("artifact=%q want /mnt/user-data/outputs/report.md", artifacts[0])
	}
}

func TestWriteTodosToolUpdatesThreadState(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.ensureSession("thread-todos", nil)

	result, err := s.todoTool().Handler(
		toolctx.WithThreadID(context.Background(), "thread-todos"),
		models.ToolCall{
			ID:   "call-todos",
			Name: "write_todos",
			Arguments: map[string]any{
				"todos": []any{
					map[string]any{"content": "Inspect repo", "status": "completed"},
					map[string]any{"content": "Implement feature", "status": "in_progress"},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("write_todos error: %v", err)
	}
	if result.Status != models.CallStatusCompleted {
		t.Fatalf("status=%s want completed", result.Status)
	}

	state := s.getThreadState("thread-todos")
	if state == nil {
		t.Fatal("state is nil")
	}
	todos, ok := state.Values["todos"].([]map[string]any)
	if !ok {
		t.Fatalf("todos type=%T", state.Values["todos"])
	}
	if len(todos) != 2 {
		t.Fatalf("todos len=%d want=2", len(todos))
	}
	if todos[1]["status"] != "in_progress" {
		t.Fatalf("todo status=%v want in_progress", todos[1]["status"])
	}
}

func TestForwardAgentEventWriteTodosEmitsUpdates(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.ensureSession("thread-1", nil)
	s.setThreadTodos("thread-1", []Todo{{Content: "Ship todo support", Status: "in_progress"}})

	rec := httptest.NewRecorder()
	run := &Run{RunID: "run-1", ThreadID: "thread-1"}

	s.forwardAgentEvent(rec, rec, run, agent.AgentEvent{
		Type:      agent.AgentEventToolCallEnd,
		MessageID: "tool-msg-1",
		Result: &models.ToolResult{
			CallID:   "call-1",
			ToolName: "write_todos",
			Status:   models.CallStatusCompleted,
			Content:  "Updated todo list",
		},
		ToolEvent: &agent.ToolCallEvent{
			ID:     "call-1",
			Name:   "write_todos",
			Status: models.CallStatusCompleted,
		},
	})

	body := rec.Body.String()
	if !strings.Contains(body, "event: updates") {
		t.Fatalf("expected updates event in %q", body)
	}
	if !strings.Contains(body, `"todos":[{"content":"Ship todo support","status":"in_progress"}]`) {
		t.Fatalf("expected todos payload in %q", body)
	}
}

func TestResolveRunConfigAddsPlanModeTodoPrompt(t *testing.T) {
	s, _ := newCompatTestServer(t)

	cfg, err := s.resolveRunConfig(runConfig{}, map[string]any{"is_plan_mode": true})
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "write_todos") {
		t.Fatalf("system prompt missing todo guidance: %q", cfg.SystemPrompt)
	}
}

func TestResolveRunConfigDisablesTaskToolWhenSubagentsDisabled(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.tools = newRuntimeToolRegistry(t)

	cfg, err := s.resolveRunConfig(runConfig{}, map[string]any{"subagent_enabled": false})
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.Tools == nil {
		t.Fatal("expected tool registry")
	}
	if tool := cfg.Tools.Get("task"); tool != nil {
		t.Fatalf("expected task tool to be removed, got %+v", tool)
	}
	if tool := cfg.Tools.Get("bash"); tool == nil {
		t.Fatal("expected unrelated tools to remain available")
	}
}

func TestResolveRunConfigKeepsTaskToolWhenSubagentsEnabled(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.tools = newRuntimeToolRegistry(t)

	cfg, err := s.resolveRunConfig(runConfig{}, map[string]any{"subagent_enabled": true})
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.Tools == nil {
		t.Fatal("expected tool registry")
	}
	if tool := cfg.Tools.Get("task"); tool == nil {
		t.Fatal("expected task tool to remain available")
	}
}

func TestUploadsAndArtifactsEndpoints(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-gateway-1"
	s.ensureSession(threadID, nil)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("hello artifact")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/uploads/list", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d", listResp.Code)
	}
	var listed struct {
		Count int              `json:"count"`
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listed.Count != 1 {
		t.Fatalf("count=%d want=1", listed.Count)
	}
	if got := asString(listed.Files[0]["path"]); got != "/mnt/user-data/uploads/hello.txt" {
		t.Fatalf("path=%q want=/mnt/user-data/uploads/hello.txt", got)
	}

	artifactResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/hello.txt", nil, nil)
	if artifactResp.Code != http.StatusOK {
		t.Fatalf("artifact status=%d", artifactResp.Code)
	}
	if artifactResp.Body.String() != "hello artifact" {
		t.Fatalf("artifact body=%q", artifactResp.Body.String())
	}
}

func TestArtifactEndpointServesHTMLInlineByDefault(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-active-artifact"
	artifactPath := filepath.Join(s.threadRoot(threadID), "outputs", "page.html")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("<html><body>x</body></html>"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/page.html", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("content-disposition=%q want empty", got)
	}
}

func TestArtifactEndpointReadsFileInsideSkillArchive(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-artifact"
	archivePath := filepath.Join(s.threadRoot(threadID), "outputs", "sample.skill")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	writeArtifactSkillArchive(t, archivePath, map[string]string{
		"notes.txt": "hello from skill",
	})

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/sample.skill/notes.txt", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp.Body.String() != "hello from skill" {
		t.Fatalf("body=%q", resp.Body.String())
	}
	if got := resp.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("content-disposition=%q want empty", got)
	}
}

func TestArtifactEndpointDownloadTrueForSkillArchive(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-download"
	archivePath := filepath.Join(s.threadRoot(threadID), "outputs", "sample.skill")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	writeArtifactSkillArchive(t, archivePath, map[string]string{
		"notes.txt": "hello from skill",
	})

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/sample.skill/notes.txt?download=true", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("content-disposition=%q want attachment", got)
	}
}

func TestArtifactEndpointReadsSkillArchiveWithTopLevelDirectory(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-prefixed"
	archivePath := filepath.Join(s.threadRoot(threadID), "outputs", "sample.skill")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	writeArtifactSkillArchive(t, archivePath, map[string]string{
		"sample-skill/SKILL.md": "# Prefixed Skill\n\nWorks.",
	})

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/sample.skill/SKILL.md", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !strings.Contains(body, "Prefixed Skill") {
		t.Fatalf("body=%q missing prefixed skill content", body)
	}
	if got := resp.Header().Get("Cache-Control"); got != "private, max-age=300" {
		t.Fatalf("cache-control=%q want private, max-age=300", got)
	}
}

func TestArtifactEndpointServesSkillArchiveHTMLInlineByDefault(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-active"
	archivePath := filepath.Join(s.threadRoot(threadID), "outputs", "sample.skill")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	writeArtifactSkillArchive(t, archivePath, map[string]string{
		"page.html": "<html><body>x</body></html>",
	})

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/sample.skill/page.html", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("content-disposition=%q want empty", got)
	}
}

func TestUploadConvertibleDocumentCreatesMarkdownCompanion(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := "thread-gateway-docx"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "report.docx")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(minimalDOCX(t, "Quarterly Review")); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	var uploaded struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if len(uploaded.Files) != 1 {
		t.Fatalf("files=%d want=1", len(uploaded.Files))
	}
	if got := asString(uploaded.Files[0]["markdown_file"]); got != "report.md" {
		t.Fatalf("markdown_file=%q want=report.md", got)
	}
	if got := asString(uploaded.Files[0]["path"]); got != "/mnt/user-data/uploads/report.docx" {
		t.Fatalf("path=%q want=/mnt/user-data/uploads/report.docx", got)
	}
	if got := asString(uploaded.Files[0]["markdown_path"]); got != "/mnt/user-data/uploads/report.md" {
		t.Fatalf("markdown_path=%q want=/mnt/user-data/uploads/report.md", got)
	}

	mdResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/report.md", nil, nil)
	if mdResp.Code != http.StatusOK {
		t.Fatalf("markdown artifact status=%d", mdResp.Code)
	}
	if !strings.Contains(mdResp.Body.String(), "Quarterly Review") {
		t.Fatalf("markdown body=%q missing extracted text", mdResp.Body.String())
	}
}

func TestDeleteConvertibleUploadRemovesMarkdownCompanion(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-delete-companion"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	original := filepath.Join(uploadDir, "report.docx")
	companion := filepath.Join(uploadDir, "report.md")
	if err := os.WriteFile(original, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}
	if err := os.WriteFile(companion, []byte("md"), 0o644); err != nil {
		t.Fatalf("write companion: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID+"/uploads/report.docx", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("expected original removed, stat err=%v", err)
	}
	if _, err := os.Stat(companion); !os.IsNotExist(err) {
		t.Fatalf("expected companion removed, stat err=%v", err)
	}
}

func TestUploadsCreateDoesNotOverwriteExistingFile(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-upload-collision"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "report.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "report.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("new")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	var uploaded struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if len(uploaded.Files) != 1 {
		t.Fatalf("files=%d want=1", len(uploaded.Files))
	}
	if got := asString(uploaded.Files[0]["filename"]); got != "report_1.txt" {
		t.Fatalf("filename=%q want=report_1.txt", got)
	}

	oldData, err := os.ReadFile(filepath.Join(uploadDir, "report.txt"))
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if string(oldData) != "old" {
		t.Fatalf("original=%q want=old", oldData)
	}

	newData, err := os.ReadFile(filepath.Join(uploadDir, "report_1.txt"))
	if err != nil {
		t.Fatalf("read deduplicated file: %v", err)
	}
	if string(newData) != "new" {
		t.Fatalf("deduplicated=%q want=new", newData)
	}
}

func TestGatewayRejectsInvalidThreadIDForFileEndpoints(t *testing.T) {
	_, handler := newCompatTestServer(t)
	const badThreadID = "bad.id"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	uploadResp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+badThreadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if uploadResp.Code != http.StatusBadRequest {
		t.Fatalf("upload status=%d body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	artifactResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+badThreadID+"/artifacts/mnt/user-data/uploads/hello.txt", nil, nil)
	if artifactResp.Code != http.StatusBadRequest {
		t.Fatalf("artifact status=%d body=%s", artifactResp.Code, artifactResp.Body.String())
	}

	deleteResp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+badThreadID, nil, nil)
	if deleteResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
}

func TestUploadArtifactURLPercentEncodesFilename(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := "thread-upload-encoded"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "report #1?.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("encoded")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	var uploaded struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if len(uploaded.Files) != 1 {
		t.Fatalf("files=%d want=1", len(uploaded.Files))
	}
	if got := asString(uploaded.Files[0]["artifact_url"]); got != "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/report%20%231%3F.txt" {
		t.Fatalf("artifact_url=%q", got)
	}
}

func TestContentDispositionEncodesUTF8Filename(t *testing.T) {
	filename := "报告 2026 #1?.pdf"
	want := "attachment; filename*=UTF-8''" + url.PathEscape(filename)
	if got := contentDisposition("attachment", filename); got != want {
		t.Fatalf("content-disposition=%q want %q", got, want)
	}
}

func TestArtifactDownloadEncodesContentDispositionFilename(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-download-filename"
	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}

	filename := "报告 2026 #1?.txt"
	if err := os.WriteFile(filepath.Join(outputDir, filename), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(
		t,
		handler,
		http.MethodGet,
		"/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/"+url.PathEscape(filename)+"?download=true",
		nil,
		nil,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	want := "attachment; filename*=UTF-8''" + url.PathEscape(filename)
	if got := resp.Header().Get("Content-Disposition"); got != want {
		t.Fatalf("content-disposition=%q want %q", got, want)
	}
}

func TestSuggestionsEndpoint(t *testing.T) {
	_, handler := newCompatTestServer(t)
	payload := `{"messages":[{"role":"user","content":"请帮我分析部署方案"}],"n":3}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}
	var data struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(data.Suggestions) == 0 {
		t.Fatal("expected non-empty suggestions")
	}
}

func TestSuggestionsEndpointUsesLLMResponse(t *testing.T) {
	provider := &titleProvider{response: "```json\n[\"Q1\",\"Q2\",\"Q3\"]\n```"}
	s, handler := newCompatTestServer(t)
	s.llmProvider = provider
	s.defaultModel = "default-model"

	payload := `{"messages":[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello"}],"n":2,"model_name":"run-model"}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}

	var data struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := strings.Join(data.Suggestions, ","); got != "Q1,Q2" {
		t.Fatalf("suggestions=%q want=%q", got, "Q1,Q2")
	}
	if provider.lastReq.Model != "run-model" {
		t.Fatalf("model=%q want=%q", provider.lastReq.Model, "run-model")
	}
	if !strings.Contains(provider.lastReq.Messages[0].Content, "User: Hi") {
		t.Fatalf("prompt missing user conversation: %q", provider.lastReq.Messages[0].Content)
	}
	if !strings.Contains(provider.lastReq.Messages[0].Content, "Assistant: Hello") {
		t.Fatalf("prompt missing assistant conversation: %q", provider.lastReq.Messages[0].Content)
	}
}

func TestParseJSONStringList(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain", raw: `["a","b"]`, want: "a,b"},
		{name: "fenced", raw: "```json\n[\"a\",\"b\"]\n```", want: "a,b"},
		{name: "wrapped", raw: "output:\n[\"a\",\"b\"]", want: "a,b"},
		{name: "invalid", raw: "nope", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(parseJSONStringList(tc.raw), ",")
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestGatewayThreadDeleteRemovesLocalData(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-delete-1"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(uploadDir, "a.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID, nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err=%v", err)
	}
}

func minimalDOCX(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create docx entry: %v", err)
	}
	content := `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>
  </w:body>
</w:document>`
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("write docx entry: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close docx zip: %v", err)
	}
	return buf.Bytes()
}

func writeArtifactSkillArchive(t *testing.T, archivePath string, files map[string]string) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}

func TestModelsSkillsMCPConfigEndpoints(t *testing.T) {
	_, handler := newCompatTestServer(t)

	modelsResp := performCompatRequest(t, handler, http.MethodGet, "/api/models", nil, nil)
	if modelsResp.Code != http.StatusOK {
		t.Fatalf("models status=%d", modelsResp.Code)
	}
	var modelsData struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.NewDecoder(modelsResp.Body).Decode(&modelsData); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if len(modelsData.Models) == 0 {
		t.Fatal("expected at least one model")
	}

	skillsResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills", nil, nil)
	if skillsResp.Code != http.StatusOK {
		t.Fatalf("skills status=%d", skillsResp.Code)
	}
	var skillsData struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.NewDecoder(skillsResp.Body).Decode(&skillsData); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(skillsData.Skills) == 0 {
		t.Fatal("expected at least one skill")
	}
	for _, skill := range skillsData.Skills {
		category, _ := skill["category"].(string)
		if category != skillCategoryPublic && category != skillCategoryCustom {
			t.Fatalf("unexpected skill category %q", category)
		}
	}

	setResp := performCompatRequest(t, handler, http.MethodPut, "/api/skills/deep-research", strings.NewReader(`{"enabled":false}`), map[string]string{"Content-Type": "application/json"})
	if setResp.Code != http.StatusOK {
		t.Fatalf("set skill status=%d", setResp.Code)
	}

	mcpResp := performCompatRequest(t, handler, http.MethodGet, "/api/mcp/config", nil, nil)
	if mcpResp.Code != http.StatusOK {
		t.Fatalf("mcp get status=%d", mcpResp.Code)
	}

	putMCPResp := performCompatRequest(t, handler, http.MethodPut, "/api/mcp/config", strings.NewReader(`{"mcp_servers":{"foo":{"enabled":true,"description":"x"}}}`), map[string]string{"Content-Type": "application/json"})
	if putMCPResp.Code != http.StatusOK {
		t.Fatalf("mcp put status=%d", putMCPResp.Code)
	}
	var mcpData struct {
		MCPServers map[string]gatewayMCPServerConfig `json:"mcp_servers"`
	}
	if err := json.NewDecoder(putMCPResp.Body).Decode(&mcpData); err != nil {
		t.Fatalf("decode mcp config: %v", err)
	}
	if !mcpData.MCPServers["foo"].Enabled {
		t.Fatal("expected foo MCP server to remain enabled")
	}

	modelGetResp := performCompatRequest(t, handler, http.MethodGet, "/api/models/qwen/Qwen3.5-9B", nil, nil)
	if modelGetResp.Code != http.StatusOK {
		t.Fatalf("model get status=%d", modelGetResp.Code)
	}
}

func TestMCPConfigRoundTripsExtendedFields(t *testing.T) {
	_, handler := newCompatTestServer(t)

	body := `{"mcp_servers":{"github":{"enabled":true,"type":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-github"],"env":{"GITHUB_TOKEN":"$TOKEN"},"headers":{"X-Test":"1"},"url":"https://example.com/mcp","description":"GitHub tools"}}}`
	resp := performCompatRequest(t, handler, http.MethodPut, "/api/mcp/config", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		MCPServers map[string]gatewayMCPServerConfig `json:"mcp_servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode put payload: %v", err)
	}
	server := payload.MCPServers["github"]
	if server.Type != "stdio" || server.Command != "npx" || server.URL != "https://example.com/mcp" {
		t.Fatalf("unexpected MCP server payload: %#v", server)
	}
	if len(server.Args) != 2 || server.Args[1] != "@modelcontextprotocol/server-github" {
		t.Fatalf("args=%#v", server.Args)
	}
	if server.Env["GITHUB_TOKEN"] != "$TOKEN" {
		t.Fatalf("env=%#v", server.Env)
	}
	if server.Headers["X-Test"] != "1" {
		t.Fatalf("headers=%#v", server.Headers)
	}
}

func TestApplyGatewayMCPConfigRegistersConnectedTools(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.tools = tools.NewRegistry()
	s.mcpConnector = func(ctx context.Context, name string, cfg gatewayMCPServerConfig) (gatewayMCPClient, error) {
		if name != "github" {
			return nil, errors.New("unexpected server")
		}
		return &fakeGatewayMCPClient{tools: []models.Tool{{
			Name:        "github.search_repos",
			Description: "Search repositories",
			Handler: func(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
				return models.ToolResult{CallID: call.ID, ToolName: call.Name, Status: models.CallStatusCompleted, Content: "ok"}, nil
			},
		}}}, nil
	}

	s.applyGatewayMCPConfig(context.Background(), gatewayMCPConfig{
		MCPServers: map[string]gatewayMCPServerConfig{
			"github": {
				Enabled: true,
				Type:    "stdio",
				Command: "npx",
			},
		},
	})

	if tool := s.tools.Get("github.search_repos"); tool == nil {
		t.Fatal("expected MCP tool to be registered")
	}

	s.applyGatewayMCPConfig(context.Background(), gatewayMCPConfig{
		MCPServers: map[string]gatewayMCPServerConfig{
			"github": {
				Enabled: false,
				Type:    "stdio",
				Command: "npx",
			},
		},
	})

	if tool := s.tools.Get("github.search_repos"); tool != nil {
		t.Fatal("expected MCP tool to be removed when server is disabled")
	}
}

func TestModelsEndpointSupportsConfiguredModelCatalogJSON(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS_JSON", `[
		{
			"id": "gpt-5",
			"name": "gpt-5",
			"model": "openai/gpt-5",
			"display_name": "GPT-5",
			"description": "Primary reasoning model",
			"supports_thinking": true,
			"supports_reasoning_effort": true
		},
		{
			"name": "deepseek-v3",
			"model": "deepseek/deepseek-v3",
			"display_name": "DeepSeek V3"
		}
	]`)

	_, handler := newCompatTestServer(t)
	resp := performCompatRequest(t, handler, http.MethodGet, "/api/models", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}

	var payload struct {
		Models []gatewayModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Models) != 2 {
		t.Fatalf("models=%d want=2", len(payload.Models))
	}
	if payload.Models[0].Name != "gpt-5" {
		t.Fatalf("unexpected models ordering/content: %#v", payload.Models)
	}
	if payload.Models[0].Model != "openai/gpt-5" {
		t.Fatalf("model=%q want=%q", payload.Models[0].Model, "openai/gpt-5")
	}
	if !payload.Models[0].SupportsThinking {
		t.Fatalf("expected explicit thinking support for %#v", payload.Models[0])
	}

	modelResp := performCompatRequest(t, handler, http.MethodGet, "/api/models/gpt-5", nil, nil)
	if modelResp.Code != http.StatusOK {
		t.Fatalf("model get status=%d", modelResp.Code)
	}
}

func TestModelsEndpointSupportsConfiguredModelCatalogList(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS", "gpt-5=openai/gpt-5, claude-3-7-sonnet=anthropic/claude-3-7-sonnet")

	_, handler := newCompatTestServer(t)
	resp := performCompatRequest(t, handler, http.MethodGet, "/api/models", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}

	var payload struct {
		Models []gatewayModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Models) != 2 {
		t.Fatalf("models=%d want=2", len(payload.Models))
	}
	if payload.Models[0].Name != "gpt-5" {
		t.Fatalf("unexpected first model: %#v", payload.Models[0])
	}
	if payload.Models[0].DisplayName != "gpt-5" {
		t.Fatalf("display_name=%q want=%q", payload.Models[0].DisplayName, "gpt-5")
	}
	if !payload.Models[1].SupportsReasoningEffort {
		t.Fatalf("expected reasoning support for %#v", payload.Models[1])
	}
}

func TestSkillInstallFromArchive(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-1"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	archivePath := filepath.Join(uploadDir, "demo.skill")
	if err := writeSkillArchive(archivePath, "demo-skill"); err != nil {
		t.Fatalf("write skill archive: %v", err)
	}

	body := `{"thread_id":"` + threadID + `","path":"/mnt/user-data/uploads/demo.skill"}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Success   bool                   `json:"success"`
		SkillName string                 `json:"skill_name"`
		Skill     map[string]interface{} `json:"skill"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode install response: %v", err)
	}
	if !payload.Success || payload.SkillName != "demo-skill" {
		t.Fatalf("unexpected install payload: %#v", payload)
	}
	if payload.Skill["category"] != skillCategoryCustom {
		t.Fatalf("skill category=%v want %s", payload.Skill["category"], skillCategoryCustom)
	}
	if payload.Skill["license"] != "MIT" {
		t.Fatalf("skill license=%v want MIT", payload.Skill["license"])
	}

	target := filepath.Join(s.dataRoot, "skills", "custom", "demo-skill", "SKILL.md")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected installed skill file: %v", err)
	}
}

func TestSkillInstallRejectsNonVirtualAndTraversalPaths(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-paths"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	archivePath := filepath.Join(uploadDir, "demo.skill")
	if err := writeSkillArchive(archivePath, "demo-skill"); err != nil {
		t.Fatalf("write skill archive: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "host absolute path",
			path: archivePath,
			want: "path must start with /mnt/user-data",
		},
		{
			name: "path traversal",
			path: "/mnt/user-data/uploads/../workspace/demo.skill",
			want: "skill file not found",
		},
		{
			name: "escape user data",
			path: "/mnt/user-data/../../etc/passwd",
			want: "access denied: path traversal detected",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"thread_id":"` + threadID + `","path":"` + tc.path + `"}`
			resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
			if resp.Code != http.StatusBadRequest && resp.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			if !strings.Contains(strings.ToLower(resp.Body.String()), tc.want) {
				t.Fatalf("body=%q want substring %q", resp.Body.String(), tc.want)
			}
		})
	}
}

func TestSkillEndpointsKeepPublicAndCustomVariantsSeparate(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-conflict"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	archivePath := filepath.Join(uploadDir, "deep-research.skill")
	if err := writeSkillArchive(archivePath, "deep-research"); err != nil {
		t.Fatalf("write skill archive: %v", err)
	}

	body := `{"thread_id":"` + threadID + `","path":"/mnt/user-data/uploads/deep-research.skill"}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("install status=%d body=%s", resp.Code, resp.Body.String())
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d", listResp.Code)
	}

	var listPayload struct {
		Skills []gatewaySkill `json:"skills"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	var categories []string
	for _, skill := range listPayload.Skills {
		if skill.Name == "deep-research" {
			categories = append(categories, skill.Category)
		}
	}
	if strings.Join(categories, ",") != "custom,public" {
		t.Fatalf("categories=%q want=%q", strings.Join(categories, ","), "custom,public")
	}

	getCustom := performCompatRequest(t, handler, http.MethodGet, "/api/skills/deep-research?category=custom", nil, nil)
	if getCustom.Code != http.StatusOK {
		t.Fatalf("get custom status=%d", getCustom.Code)
	}
	var custom gatewaySkill
	if err := json.NewDecoder(getCustom.Body).Decode(&custom); err != nil {
		t.Fatalf("decode custom skill: %v", err)
	}
	if custom.Category != skillCategoryCustom {
		t.Fatalf("custom category=%q", custom.Category)
	}

	updateResp := performCompatRequest(t, handler, http.MethodPut, "/api/skills/deep-research?category=custom", strings.NewReader(`{"enabled":false}`), map[string]string{"Content-Type": "application/json"})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update custom status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}

	publicResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills/deep-research?category=public", nil, nil)
	if publicResp.Code != http.StatusOK {
		t.Fatalf("get public status=%d", publicResp.Code)
	}
	var public gatewaySkill
	if err := json.NewDecoder(publicResp.Body).Decode(&public); err != nil {
		t.Fatalf("decode public skill: %v", err)
	}
	if !public.Enabled {
		t.Fatal("expected public skill to remain enabled")
	}
}

func TestSkillsEndpointDiscoversSkillsFromDiskRecursively(t *testing.T) {
	s, handler := newCompatTestServer(t)

	publicDir := filepath.Join(s.dataRoot, "skills", "public", "nested", "frontend-design")
	customDir := filepath.Join(s.dataRoot, "skills", "custom", "team", "release-helper")
	for _, dir := range []string{publicDir, customDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	if err := os.WriteFile(filepath.Join(publicDir, "SKILL.md"), []byte(`---
name: frontend-design
description: Design distinctive product interfaces.
license: Apache-2.0
---
# Frontend Design
`), 0o644); err != nil {
		t.Fatalf("write public skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "SKILL.md"), []byte(`---
name: release-helper
description: Prepare release checklists.
license: MIT
---
# Release Helper
`), 0o644); err != nil {
		t.Fatalf("write custom skill: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/skills", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}

	var payload struct {
		Skills []gatewaySkill `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	found := map[string]gatewaySkill{}
	for _, skill := range payload.Skills {
		found[skillStorageKey(skill.Category, skill.Name)] = skill
	}

	publicSkill, ok := found[skillStorageKey(skillCategoryPublic, "frontend-design")]
	if !ok {
		t.Fatalf("missing discovered public skill: %#v", payload.Skills)
	}
	if publicSkill.License != "Apache-2.0" {
		t.Fatalf("public license=%q want %q", publicSkill.License, "Apache-2.0")
	}

	customSkill, ok := found[skillStorageKey(skillCategoryCustom, "release-helper")]
	if !ok {
		t.Fatalf("missing discovered custom skill: %#v", payload.Skills)
	}
	if customSkill.Description != "Prepare release checklists." {
		t.Fatalf("custom description=%q", customSkill.Description)
	}
}

func TestSkillSetEnabledPersistsDiscoveredSkillState(t *testing.T) {
	s, handler := newCompatTestServer(t)

	skillDir := filepath.Join(s.dataRoot, "skills", "public", "frontend-design")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: frontend-design
description: Design distinctive product interfaces.
license: MIT
---
# Frontend Design
`), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	updateResp := performCompatRequest(t, handler, http.MethodPut, "/api/skills/frontend-design", strings.NewReader(`{"enabled":false}`), map[string]string{"Content-Type": "application/json"})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills/frontend-design", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d", getResp.Code)
	}

	var skill gatewaySkill
	if err := json.NewDecoder(getResp.Body).Decode(&skill); err != nil {
		t.Fatalf("decode skill: %v", err)
	}
	if skill.Enabled {
		t.Fatal("expected discovered skill to remain disabled after update")
	}
}

func TestAgentsAndMemoryEndpoints(t *testing.T) {
	_, handler := newCompatTestServer(t)

	createBody := `{"name":"my-agent","description":"a","model":"qwen/Qwen3.5-9B","tool_groups":["file"],"soul":"hello"}`
	createResp := performCompatRequest(t, handler, http.MethodPost, "/api/agents", strings.NewReader(createBody), map[string]string{"Content-Type": "application/json"})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create agent status=%d", createResp.Code)
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list agents status=%d", listResp.Code)
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents/my-agent", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get agent status=%d", getResp.Code)
	}

	checkResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents/check?name=my-agent", nil, nil)
	if checkResp.Code != http.StatusOK {
		t.Fatalf("check agent status=%d", checkResp.Code)
	}

	memResp := performCompatRequest(t, handler, http.MethodGet, "/api/memory", nil, nil)
	if memResp.Code != http.StatusOK {
		t.Fatalf("memory status=%d", memResp.Code)
	}

	memCfgResp := performCompatRequest(t, handler, http.MethodGet, "/api/memory/config", nil, nil)
	if memCfgResp.Code != http.StatusOK {
		t.Fatalf("memory config status=%d", memCfgResp.Code)
	}

	chResp := performCompatRequest(t, handler, http.MethodGet, "/api/channels", nil, nil)
	if chResp.Code != http.StatusOK {
		t.Fatalf("channels status=%d", chResp.Code)
	}
}

func TestRunsStreamAppliesCustomAgentRuntimeConfig(t *testing.T) {
	provider := &streamSpyProvider{}
	s := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}
	s.ensureSession("thread-custom-agent", map[string]any{"title": "Existing title"})
	modelName := "custom-model"
	s.agents["code-reviewer"] = gatewayAgent{
		Name:        "code-reviewer",
		Description: "Review code changes carefully.",
		Model:       &modelName,
		ToolGroups:  []string{"file:read", "bash"},
		Soul:        "You are a meticulous code reviewer.",
	}

	body := `{
		"thread_id":"thread-custom-agent",
		"input":{"messages":[{"role":"user","content":"Review this patch"}]},
		"context":{"agent_name":"code-reviewer"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleRunsStream(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(payload))
	}

	if provider.lastReq.Model != "custom-model" {
		t.Fatalf("model=%q want=%q", provider.lastReq.Model, "custom-model")
	}
	if !strings.Contains(provider.lastReq.SystemPrompt, "Review code changes carefully.") {
		t.Fatalf("system prompt missing description: %q", provider.lastReq.SystemPrompt)
	}
	if !strings.Contains(provider.lastReq.SystemPrompt, "You are a meticulous code reviewer.") {
		t.Fatalf("system prompt missing soul: %q", provider.lastReq.SystemPrompt)
	}

	gotTools := make([]string, 0, len(provider.lastReq.Tools))
	for _, tool := range provider.lastReq.Tools {
		gotTools = append(gotTools, tool.Name)
	}
	if strings.Join(gotTools, ",") != "ask_clarification,bash,glob,present_file,read_file" {
		t.Fatalf("tools=%q want=%q", strings.Join(gotTools, ","), "ask_clarification,bash,glob,present_file,read_file")
	}
}

func TestRunsStreamInjectsUserProfileIntoCustomAgentPrompt(t *testing.T) {
	provider := &streamSpyProvider{}
	s := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
		userProfile:  "User prefers terse code-review summaries and Go-first examples.",
	}
	s.ensureSession("thread-custom-agent-profile", map[string]any{"title": "Existing title"})
	s.agents["code-reviewer"] = gatewayAgent{
		Name:        "code-reviewer",
		Description: "Review code changes carefully.",
		Soul:        "You are a meticulous code reviewer.",
	}

	body := `{
		"thread_id":"thread-custom-agent-profile",
		"input":{"messages":[{"role":"user","content":"Review this patch"}]},
		"context":{"agent_name":"code-reviewer"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleRunsStream(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(payload))
	}

	if !strings.Contains(provider.lastReq.SystemPrompt, "USER.md:") {
		t.Fatalf("system prompt missing user profile header: %q", provider.lastReq.SystemPrompt)
	}
	if !strings.Contains(provider.lastReq.SystemPrompt, "User prefers terse code-review summaries and Go-first examples.") {
		t.Fatalf("system prompt missing user profile content: %q", provider.lastReq.SystemPrompt)
	}
}

func TestRunsStreamInjectsStoredMemoryIntoSystemPrompt(t *testing.T) {
	provider := &streamSpyProvider{}
	store := &fakeGatewayMemoryStore{
		docs: map[string]memory.Document{
			"thread-memory": {
				SessionID: "thread-memory",
				User: memory.UserMemory{
					WorkContext: "Maintains deerflow-go.",
				},
				Facts: []memory.Fact{{
					ID:        "fact-1",
					Content:   "Prefers concise technical answers.",
					Category:  "preference",
					CreatedAt: time.Now().Add(-time.Hour).UTC(),
					UpdatedAt: time.Now().Add(-time.Hour).UTC(),
				}},
				Source:    "thread-memory",
				UpdatedAt: time.Now().UTC(),
			},
		},
	}
	s := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		runStreams:   make(map[string]map[uint64]chan StreamEvent),
		dataRoot:     t.TempDir(),
		memoryStore:  store,
		memorySvc:    memory.NewService(store, fakeMemoryExtractor{}),
		agents:       map[string]gatewayAgent{},
	}

	body := `{"thread_id":"thread-memory","input":{"messages":[{"role":"user","content":"Hello"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleRunsStream(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(payload))
	}
	if !strings.Contains(provider.lastReq.SystemPrompt, "## User Memory") {
		t.Fatalf("system prompt missing memory injection: %q", provider.lastReq.SystemPrompt)
	}
	if !strings.Contains(provider.lastReq.SystemPrompt, "Maintains deerflow-go.") {
		t.Fatalf("system prompt missing work context: %q", provider.lastReq.SystemPrompt)
	}
	if !strings.Contains(provider.lastReq.SystemPrompt, "Prefers concise technical answers.") {
		t.Fatalf("system prompt missing fact: %q", provider.lastReq.SystemPrompt)
	}
}

func TestRunsStreamRejectsMissingCustomAgent(t *testing.T) {
	s := &Server{
		sessions: make(map[string]*Session),
		runs:     make(map[string]*Run),
		agents:   map[string]gatewayAgent{},
	}
	s.ensureSession("thread-missing-agent", map[string]any{"title": "Existing title"})

	body := `{
		"thread_id":"thread-missing-agent",
		"input":{"messages":[{"role":"user","content":"Hello"}]},
		"context":{"agent_name":"missing-agent"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleRunsStream(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(payload))
	}
}

func TestResolveRunConfigAllowsBootstrapForNewAgent(t *testing.T) {
	s := &Server{
		tools:  newRuntimeToolRegistry(t),
		agents: map[string]gatewayAgent{},
	}

	cfg, err := s.resolveRunConfig(runConfig{}, map[string]any{
		"is_bootstrap": true,
		"agent_name":   "code-reviewer",
	})
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.AgentName != "code-reviewer" {
		t.Fatalf("agent name=%q want=%q", cfg.AgentName, "code-reviewer")
	}
	if cfg.Tools != s.tools {
		t.Fatal("expected bootstrap flow to use server tool registry")
	}
	if !strings.Contains(cfg.SystemPrompt, "create a brand-new custom agent") {
		t.Fatalf("system prompt missing bootstrap guidance: %q", cfg.SystemPrompt)
	}
}

func TestThreadRunsCreateAndList(t *testing.T) {
	provider := &streamSpyProvider{}
	s := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		runStreams:   make(map[string]map[uint64]chan StreamEvent),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	createBody := `{"input":{"messages":[{"role":"user","content":"Hello"}]}}`
	createResp := performCompatRequest(t, mux, http.MethodPost, "/threads/thread-runs/runs", strings.NewReader(createBody), map[string]string{"Content-Type": "application/json"})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var created struct {
		RunID    string `json:"run_id"`
		ThreadID string `json:"thread_id"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ThreadID != "thread-runs" {
		t.Fatalf("thread_id=%q want=%q", created.ThreadID, "thread-runs")
	}
	if created.Status != "success" {
		t.Fatalf("status=%q want=%q", created.Status, "success")
	}
	if created.RunID == "" {
		t.Fatal("expected run_id")
	}

	listResp := performCompatRequest(t, mux, http.MethodGet, "/threads/thread-runs/runs", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}

	var listed struct {
		Runs []struct {
			RunID    string `json:"run_id"`
			ThreadID string `json:"thread_id"`
			Status   string `json:"status"`
		} `json:"runs"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Runs) != 1 {
		t.Fatalf("runs=%d want=1", len(listed.Runs))
	}
	if listed.Runs[0].RunID != created.RunID {
		t.Fatalf("listed run_id=%q want=%q", listed.Runs[0].RunID, created.RunID)
	}
}

func TestMemoryEndpointsReadAndMutateStoredDocument(t *testing.T) {
	store := &fakeGatewayMemoryStore{
		docs: map[string]memory.Document{
			"thread-memory-api": {
				SessionID: "thread-memory-api",
				User: memory.UserMemory{
					TopOfMind: "Ship the memory integration.",
				},
				Facts: []memory.Fact{{
					ID:        "fact-1",
					Content:   "User is rebuilding the Go gateway.",
					Category:  "project",
					CreatedAt: time.Now().Add(-2 * time.Hour).UTC(),
					UpdatedAt: time.Now().Add(-2 * time.Hour).UTC(),
				}},
				Source:    "thread-memory-api",
				UpdatedAt: time.Now().Add(-time.Minute).UTC(),
			},
		},
	}
	s, handler := newCompatTestServer(t)
	s.memoryStore = store
	s.memorySvc = memory.NewService(store, fakeMemoryExtractor{})
	s.memoryThread = "thread-memory-api"

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/memory", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get memory status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), "Ship the memory integration.") {
		t.Fatalf("memory body=%q", getResp.Body.String())
	}

	deleteResp := performCompatRequest(t, handler, http.MethodDelete, "/api/memory/facts/fact-1", nil, nil)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete fact status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	doc, err := store.Load(context.Background(), "thread-memory-api")
	if err != nil {
		t.Fatalf("reload store doc: %v", err)
	}
	if len(doc.Facts) != 0 {
		t.Fatalf("facts=%d want=0", len(doc.Facts))
	}

	clearResp := performCompatRequest(t, handler, http.MethodDelete, "/api/memory", nil, nil)
	if clearResp.Code != http.StatusOK {
		t.Fatalf("clear memory status=%d body=%s", clearResp.Code, clearResp.Body.String())
	}
	doc, err = store.Load(context.Background(), "thread-memory-api")
	if err != nil {
		t.Fatalf("reload cleared doc: %v", err)
	}
	if doc.User.TopOfMind != "" || len(doc.Facts) != 0 {
		t.Fatalf("cleared doc=%#v", doc)
	}
}

func TestInferBootstrapAgentNameFromBootstrapMessages(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "english",
			content:  "The new custom agent name is code-reviewer. Let's bootstrap it's SOUL.",
			expected: "code-reviewer",
		},
		{
			name:     "chinese",
			content:  "新智能体的名称是 code-reviewer，现在开始为它生成 SOUL。",
			expected: "code-reviewer",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := inferBootstrapAgentName([]models.Message{{
				ID:        "msg-1",
				SessionID: "thread-1",
				Role:      models.RoleHuman,
				Content:   tc.content,
			}})
			if got != tc.expected {
				t.Fatalf("inferred agent name=%q want=%q", got, tc.expected)
			}
		})
	}
}

func newRuntimeToolRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	registry := tools.NewRegistry()
	for _, tool := range []models.Tool{
		{Name: "bash", Groups: []string{"builtin"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "read_file", Groups: []string{"builtin", "file_ops"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "write_file", Groups: []string{"builtin", "file_ops"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "glob", Groups: []string{"builtin", "file_ops"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "present_file", Groups: []string{"builtin", "file_ops"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "ask_clarification", Groups: []string{"builtin", "interaction"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "task", Groups: []string{"agent"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register tool %q: %v", tool.Name, err)
		}
	}
	return registry
}

type streamSpyProvider struct {
	lastReq llm.ChatRequest
}

func (p *streamSpyProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (p *streamSpyProvider) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	p.lastReq = req
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{
		Done: true,
		Message: &models.Message{
			ID:        "stream-response",
			SessionID: "stream",
			Role:      models.RoleAI,
			Content:   "done",
		},
	}
	close(ch)
	return ch, nil
}

type fakeGatewayMemoryStore struct {
	docs map[string]memory.Document
}

func (f *fakeGatewayMemoryStore) AutoMigrate(context.Context) error {
	return nil
}

func (f *fakeGatewayMemoryStore) Load(_ context.Context, sessionID string) (memory.Document, error) {
	doc, ok := f.docs[sessionID]
	if !ok {
		return memory.Document{}, memory.ErrNotFound
	}
	return doc, nil
}

func (f *fakeGatewayMemoryStore) Save(_ context.Context, doc memory.Document) error {
	if f.docs == nil {
		f.docs = map[string]memory.Document{}
	}
	f.docs[doc.SessionID] = doc
	return nil
}

type fakeMemoryExtractor struct{}

func (fakeMemoryExtractor) ExtractUpdate(context.Context, memory.Document, []models.Message) (memory.Update, error) {
	return memory.Update{}, nil
}

func writeSkillArchive(path, name string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	w, err := zw.Create("my-skill/SKILL.md")
	if err != nil {
		zw.Close()
		return err
	}
	content := "---\nname: " + name + "\ndescription: Demo skill\ncategory: productivity\nlicense: MIT\n---\n# Demo\n"
	if _, err := w.Write([]byte(content)); err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}
