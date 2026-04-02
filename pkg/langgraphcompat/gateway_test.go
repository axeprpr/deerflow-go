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
	"slices"
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
	compatRoot := filepath.Join(root, "compat-root")
	if err := os.MkdirAll(compatRoot, 0o755); err != nil {
		t.Fatalf("mkdir compat root: %v", err)
	}
	dataRoot := filepath.Join(root, "data-root")
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatalf("mkdir data root: %v", err)
	}
	t.Setenv(compatRootEnv, compatRoot)
	s := &Server{
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
		runStreams: make(map[string]map[uint64]chan StreamEvent),
		dataRoot:   dataRoot,
		startedAt:  time.Now().UTC(),
		skills:     defaultGatewaySkills(),
		mcpConfig:  defaultGatewayMCPConfig(),
		agents:     map[string]gatewayAgent{},
		memory:     defaultGatewayMemory(),
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	t.Cleanup(s.waitForBackgroundTasks)
	return s, wrapTrailingSlashCompat(mux)
}

func TestGatewayOpenAPIDocumentationEndpoints(t *testing.T) {
	_, handler := newCompatTestServer(t)

	openAPIResp := performCompatRequest(t, handler, http.MethodGet, "/openapi.json", nil, nil)
	if openAPIResp.Code != http.StatusOK {
		t.Fatalf("openapi status=%d", openAPIResp.Code)
	}
	var spec struct {
		OpenAPI string         `json:"openapi"`
		Info    map[string]any `json:"info"`
		Paths   map[string]any `json:"paths"`
	}
	if err := json.NewDecoder(openAPIResp.Body).Decode(&spec); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}
	if spec.OpenAPI != "3.1.0" {
		t.Fatalf("openapi=%q want=3.1.0", spec.OpenAPI)
	}
	if spec.Info["title"] != "DeerFlow API Gateway" {
		t.Fatalf("title=%v want %q", spec.Info["title"], "DeerFlow API Gateway")
	}
	for _, path := range []string{
		"/api/models",
		"/api/threads/{thread_id}/uploads",
		"/runs/stream",
		"/threads/{thread_id}/runs/{run_id}",
		"/api/langgraph/threads/{thread_id}/runs/{run_id}",
	} {
		if _, ok := spec.Paths[path]; !ok {
			t.Fatalf("missing path %q in openapi spec", path)
		}
	}

	docsResp := performCompatRequest(t, handler, http.MethodGet, "/docs", nil, nil)
	if docsResp.Code != http.StatusOK {
		t.Fatalf("docs status=%d", docsResp.Code)
	}
	if got := docsResp.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("docs content-type=%q want html", got)
	}
	if !strings.Contains(docsResp.Body.String(), "Open raw OpenAPI schema") {
		t.Fatalf("docs body missing offline docs link: %q", docsResp.Body.String())
	}
	if !strings.Contains(docsResp.Body.String(), "/api/threads/{thread_id}/uploads") {
		t.Fatalf("docs body missing route listing: %q", docsResp.Body.String())
	}
	if strings.Contains(docsResp.Body.String(), "unpkg.com") {
		t.Fatalf("docs body unexpectedly depends on CDN: %q", docsResp.Body.String())
	}

	redocResp := performCompatRequest(t, handler, http.MethodGet, "/redoc", nil, nil)
	if redocResp.Code != http.StatusOK {
		t.Fatalf("redoc status=%d", redocResp.Code)
	}
	if got := redocResp.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("redoc content-type=%q want html", got)
	}
	if !strings.Contains(redocResp.Body.String(), "Offline route index") {
		t.Fatalf("redoc body missing offline description: %q", redocResp.Body.String())
	}
	if strings.Contains(redocResp.Body.String(), "unpkg.com") {
		t.Fatalf("redoc body unexpectedly depends on CDN: %q", redocResp.Body.String())
	}
}

func TestTrailingSlashCompatibilityForGatewayRoutes(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/channels/", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/channels/ status=%d want=%d", resp.Code, http.StatusOK)
	}

	resp = performCompatRequest(t, handler, http.MethodGet, "/api/skills/", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/skills/ status=%d want=%d", resp.Code, http.StatusOK)
	}
}

func TestTrailingSlashCompatibilityForLangGraphRoutes(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads/", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /threads/ status=%d want=%d", resp.Code, http.StatusOK)
	}

	resp = performCompatRequest(t, handler, http.MethodGet, "/api/langgraph/threads/", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/langgraph/threads/ status=%d want=%d", resp.Code, http.StatusOK)
	}
}

func TestThreadRunGetReturnsOnlyMatchingThreadRun(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-run-owner"
	otherThreadID := "thread-run-other"
	s.ensureSession(threadID, nil)
	s.ensureSession(otherThreadID, nil)

	run := &Run{
		RunID:       "run-thread-owner",
		ThreadID:    threadID,
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	resp := performCompatRequest(t, handler, http.MethodGet, "/threads/"+threadID+"/runs/"+run.RunID, nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"thread_id":"`+threadID+`"`) {
		t.Fatalf("body=%s missing thread_id", resp.Body.String())
	}

	resp = performCompatRequest(t, handler, http.MethodGet, "/threads/"+otherThreadID+"/runs/"+run.RunID, nil, nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("cross-thread status=%d body=%s", resp.Code, resp.Body.String())
	}

	resp = performCompatRequest(t, handler, http.MethodGet, "/api/langgraph/threads/"+threadID+"/runs/"+run.RunID, nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("prefixed status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestNewServerEnablesFileBackedMemoryWithoutPostgres(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if s.memorySvc == nil {
		t.Fatal("memorySvc is nil, want file-backed memory enabled")
	}
	if s.memoryStore == nil {
		t.Fatal("memoryStore is nil, want file-backed memory store")
	}
	if got := s.gatewayMemoryStoragePath(); !strings.HasSuffix(got, string(filepath.Separator)+"memory") {
		t.Fatalf("gatewayMemoryStoragePath()=%q want file-backed memory directory", got)
	}
}

func TestNewServerMatchesUpstreamSubagentConcurrency(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", t.TempDir())

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if s.subagents == nil {
		t.Fatal("subagents is nil")
	}
	if got := s.subagents.MaxConcurrent(); got != defaultGatewaySubagentMaxConcurrent {
		t.Fatalf("subagent max concurrent=%d want=%d", got, defaultGatewaySubagentMaxConcurrent)
	}
}

func TestMemoryConfigReportsFileBackedStoragePath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DEERFLOW_DATA_ROOT", root)

	s, err := NewServer(":0", "", "test-model")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	resp := performCompatRequest(t, mux, http.MethodGet, "/api/memory/config", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", resp.Code, http.StatusOK)
	}

	var body struct {
		Enabled     bool   `json:"enabled"`
		StoragePath string `json:"storage_path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Enabled {
		t.Fatal("enabled=false want true")
	}
	want := filepath.Join(root, "memory")
	if body.StoragePath != want {
		t.Fatalf("storage_path=%q want=%q", body.StoragePath, want)
	}
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

func TestMessagesToLangChainIncludesReasoningMetadataAfterNormalization(t *testing.T) {
	s, _ := newCompatTestServer(t)
	msg := llm.NormalizeAssistantMessage(models.Message{
		ID:        "ai-think",
		SessionID: "thread-think",
		Role:      models.RoleAI,
		Content:   "<think>internal reasoning</think>\n\nVisible answer",
	})

	got := s.messagesToLangChain([]models.Message{msg})
	if len(got) != 1 {
		t.Fatalf("messages=%d want=1", len(got))
	}
	if got[0].Content != "Visible answer" {
		t.Fatalf("content=%#v want Visible answer", got[0].Content)
	}
	if got[0].AdditionalKwargs["reasoning_content"] != "internal reasoning" {
		t.Fatalf("additional_kwargs=%#v want reasoning_content", got[0].AdditionalKwargs)
	}
}

func TestResolveRunConfigInjectsSkillCreatorPromptForSkillMode(t *testing.T) {
	s, _ := newCompatTestServer(t)

	skillRoot := filepath.Join(t.TempDir(), "skills")
	skillDir := filepath.Join(skillRoot, "public", "skill-creator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: skill-creator
description: Create new skills.
---
# Skill Creator

Interview the user before drafting the skill.
`), 0o644); err != nil {
		t.Fatalf("write skill creator: %v", err)
	}
	t.Setenv("DEERFLOW_SKILLS_ROOT", skillRoot)

	cfg, err := s.resolveRunConfig(runConfig{}, map[string]any{"mode": "skill"})
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "Loaded skill instructions (skill-creator):") {
		t.Fatalf("system prompt missing skill header: %q", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "Interview the user before drafting the skill.") {
		t.Fatalf("system prompt missing skill body: %q", cfg.SystemPrompt)
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
	s.ensureSession("thread-join", nil)
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

func TestThreadHistoryReturnsPersistedSnapshotsNewestFirst(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-history-multi"

	session := s.ensureSession(threadID, map[string]any{"title": "First title"})
	session.UpdatedAt = time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist first snapshot: %v", err)
	}

	session.Metadata["title"] = "Second title"
	session.Messages = []models.Message{{
		ID:        "msg-1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello history",
	}}
	session.UpdatedAt = time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist second snapshot: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/"+threadID+"/history", strings.NewReader(`{"limit":2}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var history []ThreadState
	if err := json.Unmarshal(resp.Body.Bytes(), &history); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len=%d want=2", len(history))
	}
	if got := asString(history[0].Values["title"]); got != "Second title" {
		t.Fatalf("latest title=%q want Second title", got)
	}
	if got := asString(history[1].Values["title"]); got != "First title" {
		t.Fatalf("older title=%q want First title", got)
	}
	if history[0].CheckpointID == "" || history[1].CheckpointID == "" {
		t.Fatalf("checkpoint ids must be present: %#v", history)
	}
}

func TestThreadHistorySupportsBeforeCheckpointCursor(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-history-before"

	session := s.ensureSession(threadID, map[string]any{"title": "First title"})
	session.UpdatedAt = time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist first snapshot: %v", err)
	}

	session.Metadata["title"] = "Second title"
	session.UpdatedAt = time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist second snapshot: %v", err)
	}

	session.Metadata["title"] = "Third title"
	session.UpdatedAt = time.Date(2026, 3, 31, 11, 0, 0, 0, time.UTC)
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist third snapshot: %v", err)
	}

	history := s.threadHistory(threadID)
	if len(history) < 3 {
		t.Fatalf("history len=%d want>=3", len(history))
	}

	before := history[0].CheckpointID
	resp := performCompatRequest(t, handler, http.MethodGet, "/threads/"+threadID+"/history?limit=1&before="+before, nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var got []ThreadState
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("history len=%d want=1", len(got))
	}
	if title := asString(got[0].Values["title"]); title != "Second title" {
		t.Fatalf("title=%q want Second title", title)
	}
}

func TestThreadHistoryBeforeCheckpointInRequestBody(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-history-before-body"

	session := s.ensureSession(threadID, map[string]any{"title": "First title"})
	session.UpdatedAt = time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist first snapshot: %v", err)
	}

	session.Metadata["title"] = "Second title"
	session.UpdatedAt = time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist second snapshot: %v", err)
	}

	history := s.threadHistory(threadID)
	if len(history) < 2 {
		t.Fatalf("history len=%d want>=2", len(history))
	}

	body := `{"before":"` + history[0].CheckpointID + `","limit":5}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/"+threadID+"/history", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var got []ThreadState
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("history len=%d want=2", len(got))
	}
	if title := asString(got[0].Values["title"]); title != "First title" {
		t.Fatalf("title=%q want First title", title)
	}
}

func TestThreadHistorySurvivesServerReload(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-history-reload"
	session := s.ensureSession(threadID, map[string]any{"title": "Before reload"})
	session.UpdatedAt = time.Date(2026, 3, 31, 11, 0, 0, 0, time.UTC)
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist snapshot: %v", err)
	}

	reloaded := &Server{
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
		runStreams: make(map[string]map[uint64]chan StreamEvent),
		dataRoot:   s.dataRoot,
	}
	if err := reloaded.loadPersistedSessions(); err != nil {
		t.Fatalf("load persisted sessions: %v", err)
	}

	history := reloaded.threadHistory(threadID)
	if len(history) == 0 {
		t.Fatal("expected persisted history after reload")
	}
	if got := asString(history[0].Values["title"]); got != "Before reload" {
		t.Fatalf("title=%q want Before reload", got)
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

func TestPersistedSessionsReloadOnlyAutoDiscoversOutputArtifacts(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-persisted-workspace"
	session := s.ensureSession(threadID, map[string]any{"title": "Saved thread"})
	session.Messages = []models.Message{{
		ID:        "msg-1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}
	session.UpdatedAt = time.Now().UTC()
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	outputPath := filepath.Join(s.threadRoot(threadID), "outputs", "report.md")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("# report"), 0o644); err != nil {
		t.Fatalf("write output artifact: %v", err)
	}

	workspacePath := filepath.Join(s.threadRoot(threadID), "workspace", "drafts", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(workspacePath, []byte("workspace notes"), 0o644); err != nil {
		t.Fatalf("write workspace artifact: %v", err)
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
	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if !slices.Contains(artifacts, "/mnt/user-data/outputs/report.md") {
		t.Fatalf("artifacts=%#v missing output artifact", artifacts)
	}
	if slices.Contains(artifacts, "/mnt/user-data/workspace/drafts/notes.txt") {
		t.Fatalf("artifacts=%#v unexpectedly included workspace artifact", artifacts)
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

func TestRuntimeContextFromRequestMergesConfigurableFlags(t *testing.T) {
	req := RunCreateRequest{
		Config: map[string]any{
			"configurable": map[string]any{
				"is_plan_mode":             true,
				"subagent_enabled":         false,
				"max_concurrent_subagents": 4,
				"agent_name":               "config-agent",
			},
		},
		Context: map[string]any{
			"agent_name": "context-agent",
		},
	}

	runtimeContext := runtimeContextFromRequest(req)
	if !boolFromAny(runtimeContext["is_plan_mode"]) {
		t.Fatalf("is_plan_mode=%v want true", runtimeContext["is_plan_mode"])
	}
	if boolFromAny(runtimeContext["subagent_enabled"]) {
		t.Fatalf("subagent_enabled=%v want false", runtimeContext["subagent_enabled"])
	}
	if got := stringFromAny(runtimeContext["agent_name"]); got != "context-agent" {
		t.Fatalf("agent_name=%q want context-agent", got)
	}
	if got, ok := runtimeContext["max_concurrent_subagents"].(int); !ok || got != 4 {
		t.Fatalf("max_concurrent_subagents=%#v want 4", runtimeContext["max_concurrent_subagents"])
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

func TestResolveRunConfigDisablesTaskToolByDefault(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.tools = newRuntimeToolRegistry(t)

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.Tools == nil {
		t.Fatal("expected tool registry")
	}
	if tool := cfg.Tools.Get("task"); tool != nil {
		t.Fatalf("expected task tool to be removed by default, got %+v", tool)
	}
	if tool := cfg.Tools.Get("bash"); tool == nil {
		t.Fatal("expected unrelated tools to remain available")
	}
}

func TestResolveRunConfigRemovesViewImageForNonVisionModel(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.defaultModel = "deepseek-reasoner"
	registry := tools.NewRegistry()
	for _, tool := range []models.Tool{
		{Name: "bash", Groups: []string{"builtin"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "view_image", Groups: []string{"builtin"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register tool %q: %v", tool.Name, err)
		}
	}
	s.tools = registry

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.Tools == nil {
		t.Fatal("expected tool registry")
	}
	if tool := cfg.Tools.Get("view_image"); tool != nil {
		t.Fatalf("expected view_image to be removed for non-vision model, got %+v", tool)
	}
	if tool := cfg.Tools.Get("bash"); tool == nil {
		t.Fatal("expected unrelated tools to remain available")
	}
}

func TestResolveRunConfigKeepsViewImageForVisionModel(t *testing.T) {
	s, _ := newCompatTestServer(t)
	s.defaultModel = "gpt-4.1-mini"
	registry := tools.NewRegistry()
	for _, tool := range []models.Tool{
		{Name: "bash", Groups: []string{"builtin"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "view_image", Groups: []string{"builtin"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("register tool %q: %v", tool.Name, err)
		}
	}
	s.tools = registry

	cfg, err := s.resolveRunConfig(runConfig{ModelName: "gpt-4o"}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.Tools == nil {
		t.Fatal("expected tool registry")
	}
	if tool := cfg.Tools.Get("view_image"); tool == nil {
		t.Fatal("expected view_image to remain available for vision model")
	}
}

func TestResolveRunConfigUsesConfiguredVisionSupport(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS_JSON", `[
		{"name":"custom-multimodal","model":"acme/custom-mm","supports_vision":true},
		{"name":"gpt-4o","model":"openai/gpt-4o","supports_vision":false}
	]`)

	newRegistry := func(t *testing.T) *tools.Registry {
		t.Helper()
		registry := tools.NewRegistry()
		for _, tool := range []models.Tool{
			{Name: "bash", Groups: []string{"builtin"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
			{Name: "view_image", Groups: []string{"builtin"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		} {
			if err := registry.Register(tool); err != nil {
				t.Fatalf("register tool %q: %v", tool.Name, err)
			}
		}
		return registry
	}

	t.Run("configured true keeps tool even when heuristic misses", func(t *testing.T) {
		s, _ := newCompatTestServer(t)
		s.defaultModel = "deepseek-reasoner"
		s.tools = newRegistry(t)

		cfg, err := s.resolveRunConfig(runConfig{ModelName: "custom-multimodal"}, nil)
		if err != nil {
			t.Fatalf("resolveRunConfig error: %v", err)
		}
		if tool := cfg.Tools.Get("view_image"); tool == nil {
			t.Fatal("expected view_image to remain available for configured vision model")
		}
	})

	t.Run("configured false removes tool even when heuristic matches", func(t *testing.T) {
		s, _ := newCompatTestServer(t)
		s.defaultModel = "gpt-4o"
		s.tools = newRegistry(t)

		cfg, err := s.resolveRunConfig(runConfig{ModelName: "gpt-4o"}, nil)
		if err != nil {
			t.Fatalf("resolveRunConfig error: %v", err)
		}
		if tool := cfg.Tools.Get("view_image"); tool != nil {
			t.Fatalf("expected view_image to be removed when configured model disables vision, got %+v", tool)
		}
	})
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

func TestArtifactEndpointServesPresentedSourcePathOutsideThreadRoot(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-presented-artifact"
	session := s.ensureSession(threadID, nil)

	externalDir := t.TempDir()
	externalPath := filepath.Join(externalDir, "report.md")
	if err := os.WriteFile(externalPath, []byte("# external report\n"), 0o644); err != nil {
		t.Fatalf("write external artifact: %v", err)
	}
	if err := session.PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/report.md",
		SourcePath: externalPath,
	}); err != nil {
		t.Fatalf("register present file: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/report.md", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != "# external report\n" {
		t.Fatalf("body=%q want external artifact contents", got)
	}
}

func TestArtifactEndpointRewritesMarkdownVirtualPaths(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-markdown-rewrite"
	artifactPath := filepath.Join(s.threadRoot(threadID), "outputs", "report.md")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	content := strings.Join([]string{
		"# Report",
		"",
		"![Chart](/mnt/user-data/outputs/chart final.png)",
		"",
		"[Source](/mnt/user-data/uploads/source data.csv)",
		"",
		"Raw path: /mnt/user-data/workspace/notes/todo.txt",
		"",
	}, "\n")
	if err := os.WriteFile(artifactPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/report.md", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if strings.Contains(body, "/mnt/user-data/outputs/chart final.png") {
		t.Fatalf("body=%q still contains raw outputs virtual path", body)
	}
	if !strings.Contains(body, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/chart%20final.png") {
		t.Fatalf("body=%q missing rewritten outputs path", body)
	}
	if !strings.Contains(body, "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/source%20data.csv") {
		t.Fatalf("body=%q missing rewritten uploads path", body)
	}
	if !strings.Contains(body, "/api/threads/"+threadID+"/artifacts/mnt/user-data/workspace/notes/todo.txt") {
		t.Fatalf("body=%q missing rewritten workspace path", body)
	}
}

func TestArtifactEndpointDownloadKeepsOriginalMarkdownPaths(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-markdown-download"
	artifactPath := filepath.Join(s.threadRoot(threadID), "outputs", "report.md")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	content := "![Chart](/mnt/user-data/outputs/chart.png)\n"
	if err := os.WriteFile(artifactPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/report.md?download=true", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != content {
		t.Fatalf("body=%q want original markdown body", got)
	}
}

func TestArtifactEndpointServesUploadMarkdownPreviewForOfficeAndPDFFiles(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-upload-preview"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.pdf"), []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	const preview = "# Brief\n\nConverted preview.\n"
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.md"), []byte(preview), 0o644); err != nil {
		t.Fatalf("write upload preview: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/brief.pdf", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != preview {
		t.Fatalf("body=%q want markdown preview", got)
	}
	if contentType := resp.Header().Get("Content-Type"); !strings.Contains(contentType, "text/markdown") {
		t.Fatalf("content-type=%q want markdown", contentType)
	}
}

func TestArtifactEndpointDownloadKeepsOriginalUploadFileWhenMarkdownPreviewExists(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-upload-preview-download"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads dir: %v", err)
	}
	const original = "%PDF-1.7"
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.pdf"), []byte(original), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.md"), []byte("# Brief\n\nConverted preview.\n"), 0o644); err != nil {
		t.Fatalf("write upload preview: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/brief.pdf?download=true", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != original {
		t.Fatalf("body=%q want original upload", got)
	}
	if disposition := resp.Header().Get("Content-Disposition"); !strings.Contains(disposition, "brief.pdf") {
		t.Fatalf("content-disposition=%q want original filename", disposition)
	}
}

func TestUploadsEndpointIgnoresMarkdownConversionFailures(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-upload-conversion-failure"
	s.ensureSession(threadID, nil)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "broken.docx")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("not a zip archive")); err != nil {
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
		Success bool             `json:"success"`
		Files   []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if !uploaded.Success {
		t.Fatal("expected upload success")
	}
	if len(uploaded.Files) != 1 {
		t.Fatalf("files=%d want=1", len(uploaded.Files))
	}
	if got := asString(uploaded.Files[0]["filename"]); got != "broken.docx" {
		t.Fatalf("filename=%q want=broken.docx", got)
	}
	if _, ok := uploaded.Files[0]["markdown_file"]; ok {
		t.Fatalf("markdown_file=%v want omitted on conversion failure", uploaded.Files[0]["markdown_file"])
	}

	artifactResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/broken.docx", nil, nil)
	if artifactResp.Code != http.StatusOK {
		t.Fatalf("artifact status=%d body=%s", artifactResp.Code, artifactResp.Body.String())
	}
	if got := artifactResp.Body.String(); got != "not a zip archive" {
		t.Fatalf("artifact body=%q want original file content", got)
	}
}

func TestUploadsEndpointReturnsListCompatibleMetadata(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-upload-metadata"
	s.ensureSession(threadID, nil)

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

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	var uploaded struct {
		Success bool             `json:"success"`
		Files   []map[string]any `json:"files"`
		Message string           `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if !uploaded.Success {
		t.Fatal("expected upload success")
	}
	if uploaded.Message != "Successfully uploaded 1 file(s)" {
		t.Fatalf("message=%q want=%q", uploaded.Message, "Successfully uploaded 1 file(s)")
	}
	if len(uploaded.Files) != 1 {
		t.Fatalf("files=%d want=1", len(uploaded.Files))
	}
	if got := asString(uploaded.Files[0]["extension"]); got != ".txt" {
		t.Fatalf("extension=%q want=.txt", got)
	}
	if got := toInt64(uploaded.Files[0]["modified"]); got <= 0 {
		t.Fatalf("modified=%d want > 0", got)
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/uploads/list", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}

	var listed struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Files) != 1 {
		t.Fatalf("listed files=%d want=1", len(listed.Files))
	}
	if got := asString(listed.Files[0]["extension"]); got != asString(uploaded.Files[0]["extension"]) {
		t.Fatalf("listed extension=%q want upload response extension=%q", got, asString(uploaded.Files[0]["extension"]))
	}
}

func TestValidateUploadedFilenameNormalizesDirectoryPathsToBasename(t *testing.T) {
	got, err := validateUploadedFilename("nested/report.txt")
	if err != nil {
		t.Fatalf("validateUploadedFilename error: %v", err)
	}
	if got != "report.txt" {
		t.Fatalf("filename=%q want=%q", got, "report.txt")
	}
}

func TestValidateUploadedFilenameNormalizesURLHostileCharacters(t *testing.T) {
	got, err := validateUploadedFilename("Q2 plan #1?.pdf")
	if err != nil {
		t.Fatalf("validateUploadedFilename error: %v", err)
	}
	if got != "Q2 plan _1_.pdf" {
		t.Fatalf("filename=%q want=%q", got, "Q2 plan _1_.pdf")
	}
}

func TestValidateUploadedFilenameRejectsTooLongNames(t *testing.T) {
	name := strings.Repeat("a", 252) + ".txt"
	_, err := validateUploadedFilename(name)
	if err == nil {
		t.Fatal("expected long filename to be rejected")
	}
	if !strings.Contains(err.Error(), "filename too long") {
		t.Fatalf("err=%q want too long error", err)
	}
}

func TestUploadsEndpointRollsBackWrittenFilesOnSaveFailure(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-upload-rollback"
	s.ensureSession(threadID, nil)

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	blockedPath := filepath.Join(uploadDir, "blocked.txt")
	if err := os.MkdirAll(blockedPath, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	firstPart, err := w.CreateFormFile("files", "first.txt")
	if err != nil {
		t.Fatalf("create first form file: %v", err)
	}
	if _, err := firstPart.Write([]byte("first artifact")); err != nil {
		t.Fatalf("write first form file: %v", err)
	}
	secondPart, err := w.CreateFormFile("files", "blocked.txt")
	if err != nil {
		t.Fatalf("create second form file: %v", err)
	}
	if _, err := secondPart.Write([]byte("should fail")); err != nil {
		t.Fatalf("write second form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	if _, err := os.Stat(filepath.Join(uploadDir, "first.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected first.txt to be rolled back, stat err=%v", err)
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/uploads/list", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listed struct {
		Count int              `json:"count"`
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listed.Count != 0 || len(listed.Files) != 0 {
		t.Fatalf("expected empty uploads after rollback, got count=%d files=%d", listed.Count, len(listed.Files))
	}
}

func TestArtifactEndpointForcesDownloadForHTML(t *testing.T) {
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
	if got := resp.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("content-disposition=%q want attachment", got)
	}
}

func TestArtifactEndpointForcesDownloadForXHTML(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-active-xhtml-artifact"
	artifactPath := filepath.Join(s.threadRoot(threadID), "outputs", "page.xhtml")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte(`<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><body>x</body></html>`), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/page.xhtml", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("content-disposition=%q want attachment", got)
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

func TestArtifactEndpointForcesDownloadForSVGInSkillArchive(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-active"
	archivePath := filepath.Join(s.threadRoot(threadID), "outputs", "sample.skill")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	writeArtifactSkillArchive(t, archivePath, map[string]string{
		"chart.svg": `<svg xmlns="http://www.w3.org/2000/svg"/>`,
	})

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/sample.skill/chart.svg", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("content-disposition=%q want attachment", got)
	}
}

func TestArtifactEndpointForcesDownloadForSVG(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-active-artifact"
	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "chart.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	resp := performCompatRequest(
		t,
		handler,
		http.MethodGet,
		"/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/chart.svg",
		nil,
		nil,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("content-disposition=%q want attachment", got)
	}
}

func TestArtifactEndpointSupportsRangeRequests(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-artifact-range"
	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "movie.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/movie.txt", nil)
	req.Header.Set("Range", "bytes=6-10")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusPartialContent {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != "world" {
		t.Fatalf("body=%q want=world", got)
	}
	if got := resp.Header().Get("Content-Range"); got != "bytes 6-10/11" {
		t.Fatalf("content-range=%q want bytes 6-10/11", got)
	}
}

func TestArtifactEndpointRejectsSymlinkEscapingThreadRoot(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-artifact-symlink"
	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(outputDir, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/escape.txt", nil, nil)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "access denied: path traversal detected") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestArtifactEndpointRejectsPathTraversalWithForbidden(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-artifact-traversal"
	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "ok.txt"), []byte("visible"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/outputs/%2e%2e/%2e%2e/secrets.txt", nil, nil)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "access denied: path traversal detected") {
		t.Fatalf("body=%q", resp.Body.String())
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

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/uploads/list", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}

	var listed struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	var report map[string]any
	for _, file := range listed.Files {
		if asString(file["filename"]) == "report.docx" {
			report = file
			break
		}
	}
	if report == nil {
		t.Fatalf("list missing report.docx entry: %#v", listed.Files)
	}
	if len(listed.Files) != 1 {
		t.Fatalf("listed files=%#v want only original upload entry", listed.Files)
	}
	if got := asString(report["markdown_file"]); got != "report.md" {
		t.Fatalf("list markdown_file=%q want=report.md", got)
	}
	if got := asString(report["markdown_virtual_path"]); got != "/mnt/user-data/uploads/report.md" {
		t.Fatalf("list markdown_virtual_path=%q want=/mnt/user-data/uploads/report.md", got)
	}
	if got := asString(report["markdown_artifact_url"]); got != "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/report.md" {
		t.Fatalf("list markdown_artifact_url=%q want markdown artifact path", got)
	}
}

func TestUploadsCreateMakesFilesSandboxWritable(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-upload-permissions"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "report.docx")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(minimalDOCX(t, "Writable Upload")); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	for _, name := range []string{"report.docx", "report.md"} {
		info, err := os.Stat(filepath.Join(s.uploadsDir(threadID), name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := info.Mode().Perm() & 0o222; got != 0o222 {
			t.Fatalf("%s mode=%#o want world-writable", name, info.Mode().Perm())
		}
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
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if got := asString(payload["message"]); got != "Deleted report.docx" {
		t.Fatalf("message=%q want=Deleted report.docx", got)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("expected original removed, stat err=%v", err)
	}
	if _, err := os.Stat(companion); !os.IsNotExist(err) {
		t.Fatalf("expected companion removed, stat err=%v", err)
	}
}

func TestDeleteUploadReturnsNotFoundForMissingFile(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := "thread-delete-upload-missing"

	resp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID+"/uploads/missing.txt", nil, nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "File not found: missing.txt") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestDeleteUploadRejectsSymlinkEscapingUploadsDir(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-delete-upload-symlink"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(uploadDir, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID+"/uploads/escape.txt", nil, nil)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "access denied: path traversal detected") {
		t.Fatalf("body=%q", resp.Body.String())
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
	if got := asString(uploaded.Files[0]["original_filename"]); got != "report.txt" {
		t.Fatalf("original_filename=%q want=report.txt", got)
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

func TestUploadsCreateReturnsOriginalFilenameForInRequestDuplicates(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := "thread-upload-triple-duplicates"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, content := range []string{"version A", "version B", "version C"} {
		part, err := w.CreateFormFile("files", "report.csv")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatalf("write form file: %v", err)
		}
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
	if len(uploaded.Files) != 3 {
		t.Fatalf("files=%d want=3", len(uploaded.Files))
	}

	gotNames := []string{
		asString(uploaded.Files[0]["filename"]),
		asString(uploaded.Files[1]["filename"]),
		asString(uploaded.Files[2]["filename"]),
	}
	if strings.Join(gotNames, ",") != "report.csv,report_1.csv,report_2.csv" {
		t.Fatalf("filenames=%v", gotNames)
	}
	if _, exists := uploaded.Files[0]["original_filename"]; exists {
		t.Fatalf("unexpected original_filename on first file: %#v", uploaded.Files[0])
	}
	for i := 1; i < 3; i++ {
		if got := asString(uploaded.Files[i]["original_filename"]); got != "report.csv" {
			t.Fatalf("files[%d].original_filename=%q want=report.csv", i, got)
		}
	}
}

func TestUploadsCreateSkipsUnsafeFilenames(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := "thread-upload-skip-unsafe"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, name := range []string{".", ".."} {
		part, err := w.CreateFormFile("files", name)
		if err != nil {
			t.Fatalf("create form file %q: %v", name, err)
		}
		if _, err := part.Write([]byte("ignored")); err != nil {
			t.Fatalf("write form file %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	var uploaded struct {
		Success bool             `json:"success"`
		Files   []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if !uploaded.Success {
		t.Fatal("expected upload success")
	}
	if len(uploaded.Files) != 0 {
		t.Fatalf("files=%d want=0", len(uploaded.Files))
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/uploads/list", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listed struct {
		Count int              `json:"count"`
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listed.Count != 0 || len(listed.Files) != 0 {
		t.Fatalf("expected empty uploads, got count=%d files=%d", listed.Count, len(listed.Files))
	}
}

func TestUploadsCreateNormalizesPathLikeFilenameToBasename(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-upload-basename"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "../etc/passwd")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("safe")); err != nil {
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
	if got := asString(uploaded.Files[0]["filename"]); got != "passwd" {
		t.Fatalf("filename=%q want=passwd", got)
	}

	data, err := os.ReadFile(filepath.Join(s.uploadsDir(threadID), "passwd"))
	if err != nil {
		t.Fatalf("read normalized upload: %v", err)
	}
	if string(data) != "safe" {
		t.Fatalf("content=%q want=safe", string(data))
	}
}

func TestGatewayRejectsInvalidThreadIDForFileEndpoints(t *testing.T) {
	_, handler := newCompatTestServer(t)
	const badThreadID = "bad!id"

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

func TestGatewayThreadDeleteRemovesOnlyLocalData(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-delete-local"
	session := s.ensureSession(threadID, map[string]any{"title": "Delete me"})
	session.Messages = []models.Message{{
		ID:        "msg-1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}
	if err := s.persistSessionSnapshot(cloneSession(session)); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	outputFile := filepath.Join(s.threadRoot(threadID), "outputs", "report.md")
	if err := os.MkdirAll(filepath.Dir(outputFile), 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(outputFile, []byte("report"), 0o644); err != nil {
		t.Fatalf("write output file: %v", err)
	}
	if err := session.PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/report.md",
		SourcePath: outputFile,
	}); err != nil {
		t.Fatalf("register present file: %v", err)
	}

	workspaceFile := filepath.Join(s.threadRoot(threadID), "workspace", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(workspaceFile), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(workspaceFile, []byte("cleanup"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	uploadFile := filepath.Join(s.uploadsDir(threadID), "notes.txt")
	if err := os.MkdirAll(filepath.Dir(uploadFile), 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(uploadFile, []byte("upload"), 0o644); err != nil {
		t.Fatalf("write upload file: %v", err)
	}
	runID := "run-delete-local"
	s.saveRun(&Run{
		RunID:       runID,
		ThreadID:    threadID,
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})

	resp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID, nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success {
		t.Fatal("expected delete success")
	}
	if payload.Message != "Deleted local thread data for "+threadID {
		t.Fatalf("message=%q", payload.Message)
	}

	if _, err := os.Stat(s.threadDir(threadID)); err != nil {
		t.Fatalf("expected thread dir to remain for persisted state, stat err=%v", err)
	}
	if _, err := os.Stat(s.threadRoot(threadID)); !os.IsNotExist(err) {
		t.Fatalf("expected user-data removed, stat err=%v", err)
	}
	if _, err := os.Stat(s.sessionStatePath(threadID)); err != nil {
		t.Fatalf("expected session state preserved, stat err=%v", err)
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/threads/"+threadID, nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}

	runResp := performCompatRequest(t, handler, http.MethodGet, "/runs/"+runID, nil, nil)
	if runResp.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", runResp.Code, runResp.Body.String())
	}

	var threadPayload struct {
		Values map[string]any `json:"values"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&threadPayload); err != nil {
		t.Fatalf("decode thread response: %v", err)
	}
	if artifacts, _ := threadPayload.Values["artifacts"].([]any); len(artifacts) != 0 {
		t.Fatalf("artifacts=%#v want empty after gateway cleanup", artifacts)
	}
	if uploads, _ := threadPayload.Values["uploaded_files"].([]any); len(uploads) != 0 {
		t.Fatalf("uploaded_files=%#v want empty after gateway cleanup", uploads)
	}

	reloaded := &Server{
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
		runStreams: make(map[string]map[uint64]chan StreamEvent),
		dataRoot:   s.dataRoot,
	}
	if err := reloaded.loadPersistedSessions(); err != nil {
		t.Fatalf("load persisted sessions: %v", err)
	}

	state := reloaded.getThreadState(threadID)
	if state == nil {
		t.Fatal("expected thread state after reload")
	}
	if artifacts, _ := state.Values["artifacts"].([]string); len(artifacts) != 0 {
		t.Fatalf("reloaded artifacts=%#v want empty after cleanup", artifacts)
	}
	if uploads, _ := state.Values["uploaded_files"].([]map[string]any); len(uploads) != 0 {
		t.Fatalf("reloaded uploaded_files=%#v want empty after cleanup", uploads)
	}
}

func TestGatewayThreadDeleteIsIdempotentForMissingThreadData(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := "thread-delete-missing"

	resp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID, nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("first delete status=%d body=%s", resp.Code, resp.Body.String())
	}

	resp = performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID, nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("second delete status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestThreadDeleteRemovesRunsAndLocalThreadData(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-delete-all"
	s.ensureSession(threadID, map[string]any{"title": "Delete everything"})
	runID := "run-thread-delete"
	s.saveRun(&Run{
		RunID:       runID,
		ThreadID:    threadID,
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})

	workspaceFile := filepath.Join(s.threadRoot(threadID), "workspace", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(workspaceFile), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(workspaceFile, []byte("cleanup"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodDelete, "/threads/"+threadID, nil, nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	if _, err := os.Stat(s.threadDir(threadID)); !os.IsNotExist(err) {
		t.Fatalf("expected thread dir removed, stat err=%v", err)
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/threads/"+threadID, nil, nil)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}

	runResp := performCompatRequest(t, handler, http.MethodGet, "/runs/"+runID, nil, nil)
	if runResp.Code != http.StatusNotFound {
		t.Fatalf("run status=%d body=%s", runResp.Code, runResp.Body.String())
	}
}

func TestThreadDeleteRemovesThreadScopedMemoryAndClearsCache(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-delete-memory"
	store := &fakeGatewayMemoryStore{
		docs: map[string]memory.Document{
			threadID: {
				SessionID: threadID,
				Source:    threadID,
				User: memory.UserMemory{
					TopOfMind: "Remove this memory.",
				},
			},
			"agent:writer-bot": {
				SessionID: "agent:writer-bot",
				Source:    "agent:writer-bot",
				User: memory.UserMemory{
					TopOfMind: "Keep agent memory.",
				},
			},
		},
	}
	s.memoryStore = store
	s.memorySvc = memory.NewService(store, fakeMemoryExtractor{})
	s.memoryThread = threadID
	s.memory = gatewayMemoryResponse{
		Version:     "1",
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
		User: memoryUser{
			TopOfMind: memorySection{
				Summary:   "Remove this memory.",
				UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	s.ensureSession(threadID, map[string]any{"title": "Delete memory"})

	resp := performCompatRequest(t, handler, http.MethodDelete, "/threads/"+threadID, nil, nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	if _, err := store.Load(context.Background(), threadID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("load deleted thread memory err=%v want ErrNotFound", err)
	}
	if _, err := store.Load(context.Background(), "agent:writer-bot"); err != nil {
		t.Fatalf("agent memory should remain: %v", err)
	}
	if got := s.memoryThread; got != "" {
		t.Fatalf("memoryThread=%q want empty", got)
	}

	memResp := performCompatRequest(t, handler, http.MethodGet, "/api/memory", nil, nil)
	if memResp.Code != http.StatusOK {
		t.Fatalf("memory status=%d body=%s", memResp.Code, memResp.Body.String())
	}
	if strings.Contains(memResp.Body.String(), "Remove this memory.") {
		t.Fatalf("deleted thread memory leaked into gateway cache: %q", memResp.Body.String())
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
	if got := asString(uploaded.Files[0]["filename"]); got != "report _1_.txt" {
		t.Fatalf("filename=%q want=%q", got, "report _1_.txt")
	}
	if got := asString(uploaded.Files[0]["artifact_url"]); got != "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/report%20_1_.txt" {
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

func TestSuggestionsEndpointIncludesThreadContextHints(t *testing.T) {
	provider := &titleProvider{response: `["先总结需求文档"]`}
	s, handler := newCompatTestServer(t)
	s.llmProvider = provider
	s.defaultModel = "default-model"
	s.sessions["t1"] = &Session{
		ThreadID: "t1",
		Metadata: map[string]any{
			"title":      "客户门户改版",
			"agent_name": "writer-bot",
		},
		PresentFiles: tools.NewPresentFileRegistry(),
	}
	s.agents["writer-bot"] = gatewayAgent{
		Name:        "writer-bot",
		Description: "擅长整理需求和交付文档",
	}

	outputDir := filepath.Join(s.threadRoot("t1"), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	artifactPath := filepath.Join(outputDir, "spec-summary.md")
	if err := os.WriteFile(artifactPath, []byte("# summary"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := s.sessions["t1"].PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/spec-summary.md",
		SourcePath: artifactPath,
	}); err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	uploadDir := s.uploadsDir("t1")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	payload := `{"messages":[{"role":"user","content":"帮我梳理客户门户改版需求"},{"role":"assistant","content":"我先整理范围。"}],"n":1}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	prompt := provider.lastReq.Messages[0].Content
	for _, want := range []string{
		"Thread title: 客户门户改版",
		"Custom agent: writer-bot - 擅长整理需求和交付文档",
		"Uploaded files: requirements.pdf",
		"Generated artifacts: spec-summary.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
}

func TestSuggestionsEndpointFallsBackToThreadContextWithoutConversationText(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.sessions["t1"] = &Session{
		ThreadID: "t1",
		Metadata: map[string]any{
			"title": "Migration planning",
		},
		PresentFiles: tools.NewPresentFileRegistry(),
	}

	uploadDir := s.uploadsDir("t1")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	payload := `{"messages":[{"role":"user","content":"   "}],"n":2}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var data struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := strings.Join(data.Suggestions, ","); got != "Summarize the key points from these uploaded files first.,What should I pay attention to first in these files?" {
		t.Fatalf("suggestions=%q", got)
	}
}

func TestSuggestionsEndpointFallsBackToGenericContextWithoutThreadHints(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.llmProvider = &titleProvider{err: errors.New("llm unavailable")}

	payload := `{"messages":[{"role":"assistant","content":"   "}],"n":2}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/t1/suggestions", strings.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var data struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := strings.Join(data.Suggestions, ","); got != "Based on the current thread context, what should I do next?,Summarize the key conclusions and open questions in this thread." {
		t.Fatalf("suggestions=%q", got)
	}
}

func TestGatewayThreadFilesEndpointListsUploadsWorkspaceAndArtifacts(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-files-gateway"
	session := s.ensureSession(threadID, nil)

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	reportPath := filepath.Join(outputDir, "report.md")
	if err := os.WriteFile(reportPath, []byte("# report"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := session.PresentFiles.Register(tools.PresentFile{
		Path:       "/mnt/user-data/outputs/report.md",
		SourcePath: reportPath,
	}); err != nil {
		t.Fatalf("register present file: %v", err)
	}

	workspaceDir := filepath.Join(s.threadRoot(threadID), "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.md"), []byte("# requirements"), 0o644); err != nil {
		t.Fatalf("write upload markdown companion: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/files", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Files []tools.PresentFile `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	got := make([]string, 0, len(payload.Files))
	for _, file := range payload.Files {
		got = append(got, file.Path)
	}
	want := []string{
		"/mnt/user-data/outputs/report.md",
		"/mnt/user-data/workspace/notes.txt",
		"/mnt/user-data/uploads/requirements.md",
		"/mnt/user-data/uploads/requirements.pdf",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files=%v want=%v", got, want)
	}
}

func TestGatewayThreadFilesEndpointFallsBackToDiskWithoutSession(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-files-disk-only"

	outputDir := filepath.Join(s.threadRoot(threadID), "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "report.md"), []byte("# report"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	workspaceDir := filepath.Join(s.threadRoot(threadID), "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "requirements.md"), []byte("# requirements"), 0o644); err != nil {
		t.Fatalf("write upload markdown companion: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/files", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Files []tools.PresentFile `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	got := make([]string, 0, len(payload.Files))
	for _, file := range payload.Files {
		got = append(got, file.Path)
	}
	want := []string{
		"/mnt/user-data/outputs/report.md",
		"/mnt/user-data/workspace/notes.txt",
		"/mnt/user-data/uploads/requirements.md",
		"/mnt/user-data/uploads/requirements.pdf",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files=%v want=%v", got, want)
	}
}

func TestAugmentToolRuntimeContext(t *testing.T) {
	runtimeContext := map[string]any{
		"thinking_enabled": true,
	}

	got := augmentToolRuntimeContext(runtimeContext, runConfig{
		ModelName:       "gpt-5",
		ReasoningEffort: "minimal",
	}, "<skill_system>\nRead skills first.\n</skill_system>")

	if got["thinking_enabled"] != true {
		t.Fatalf("thinking_enabled=%#v want true", got["thinking_enabled"])
	}
	if got["model_name"] != "gpt-5" {
		t.Fatalf("model_name=%#v want gpt-5", got["model_name"])
	}
	if got["reasoning_effort"] != "minimal" {
		t.Fatalf("reasoning_effort=%#v want minimal", got["reasoning_effort"])
	}
	if got["skills_prompt"] != "<skill_system>\nRead skills first.\n</skill_system>" {
		t.Fatalf("skills_prompt=%#v", got["skills_prompt"])
	}
	if _, exists := runtimeContext["model_name"]; exists {
		t.Fatalf("augmentToolRuntimeContext mutated input: %#v", runtimeContext)
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
	t.Setenv("DEERFLOW_DATA_ROOT", s.dataRoot)
	threadID := "thread-delete-1"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(uploadDir, "a.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	acpDir, err := tools.ACPWorkspaceDir(threadID)
	if err != nil {
		t.Fatalf("acp workspace dir: %v", err)
	}
	acpFile := filepath.Join(acpDir, "artifact.txt")
	if err := os.MkdirAll(acpDir, 0o755); err != nil {
		t.Fatalf("mkdir acp dir: %v", err)
	}
	if err := os.WriteFile(acpFile, []byte("acp"), 0o644); err != nil {
		t.Fatalf("write acp file: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID, nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d", resp.Code)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err=%v", err)
	}
	if _, err := os.Stat(acpFile); !os.IsNotExist(err) {
		t.Fatalf("expected ACP file removed, stat err=%v", err)
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

	body := `{"mcp_servers":{"github":{"enabled":true,"type":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-github"],"env":{"GITHUB_TOKEN":"$TOKEN"},"headers":{"X-Test":"1"},"url":"https://example.com/mcp","oauth":{"enabled":true,"token_url":"https://auth.example.com/token","grant_type":"client_credentials","client_id":"demo-client","client_secret":"demo-secret","scope":"repo","audience":"mcp","token_field":"access_token","token_type_field":"token_type","expires_in_field":"expires_in","default_token_type":"Bearer","refresh_skew_seconds":45,"extra_token_params":{"resource":"github"}},"description":"GitHub tools"}}}`
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
	if server.OAuth == nil || server.OAuth.TokenURL != "https://auth.example.com/token" {
		t.Fatalf("oauth=%#v", server.OAuth)
	}
	if server.OAuth.ExtraTokenParams["resource"] != "github" {
		t.Fatalf("oauth extra params=%#v", server.OAuth.ExtraTokenParams)
	}
}

func TestLoadGatewayStateReadsExtensionsConfigCompatibilityFile(t *testing.T) {
	s, handler := newCompatTestServer(t)
	configPath := filepath.Join(t.TempDir(), "extensions_config.json")
	t.Setenv("DEERFLOW_EXTENSIONS_CONFIG_PATH", configPath)

	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "github": {
      "enabled": true,
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "description": "GitHub tools"
    }
  },
  "skills": {
    "deep-research": {
      "enabled": false
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write extensions config: %v", err)
	}

	if err := s.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	skillResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills/deep-research", nil, nil)
	if skillResp.Code != http.StatusOK {
		t.Fatalf("skill status=%d body=%s", skillResp.Code, skillResp.Body.String())
	}
	var skill gatewaySkill
	if err := json.NewDecoder(skillResp.Body).Decode(&skill); err != nil {
		t.Fatalf("decode skill: %v", err)
	}
	if skill.Enabled {
		t.Fatal("expected extensions_config.json to disable deep-research")
	}

	mcpResp := performCompatRequest(t, handler, http.MethodGet, "/api/mcp/config", nil, nil)
	if mcpResp.Code != http.StatusOK {
		t.Fatalf("mcp status=%d body=%s", mcpResp.Code, mcpResp.Body.String())
	}
	var mcp gatewayMCPConfig
	if err := json.NewDecoder(mcpResp.Body).Decode(&mcp); err != nil {
		t.Fatalf("decode mcp: %v", err)
	}
	if !mcp.MCPServers["github"].Enabled {
		t.Fatal("expected github MCP server to be enabled from extensions_config.json")
	}
}

func TestSkillsGetRefreshesExtensionsConfigCompatibilityFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "extensions_config.json")
	t.Setenv("DEERFLOW_EXTENSIONS_CONFIG_PATH", configPath)

	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {},
  "skills": {
    "deep-research": {
      "enabled": false
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write initial extensions config: %v", err)
	}

	_, handler := newCompatTestServer(t)

	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {},
  "skills": {
    "deep-research": {
      "enabled": true
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write updated extensions config: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/skills/deep-research", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var skill gatewaySkill
	if err := json.NewDecoder(resp.Body).Decode(&skill); err != nil {
		t.Fatalf("decode skill: %v", err)
	}
	if !skill.Enabled {
		t.Fatalf("skill.Enabled=%v want true after refresh", skill.Enabled)
	}
}

func TestSkillSetEnabledPersistsExtensionsConfigCompatibilityFile(t *testing.T) {
	_, handler := newCompatTestServer(t)
	configPath := filepath.Join(t.TempDir(), "extensions_config.json")
	t.Setenv("DEERFLOW_EXTENSIONS_CONFIG_PATH", configPath)

	resp := performCompatRequest(t, handler, http.MethodPut, "/api/skills/deep-research", strings.NewReader(`{"enabled":false}`), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read extensions config: %v", err)
	}
	var cfg gatewayExtensionsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode extensions config: %v", err)
	}
	state, ok := cfg.Skills["deep-research"]
	if !ok {
		t.Fatalf("missing deep-research in skills config: %s", string(data))
	}
	if state.Enabled {
		t.Fatalf("deep-research enabled=%v want false", state.Enabled)
	}
}

func TestMCPConfigPutPersistsExtensionsConfigCompatibilityFile(t *testing.T) {
	_, handler := newCompatTestServer(t)
	configPath := filepath.Join(t.TempDir(), "extensions_config.json")
	t.Setenv("DEERFLOW_EXTENSIONS_CONFIG_PATH", configPath)

	body := `{"mcp_servers":{"github":{"enabled":true,"type":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-github"],"description":"GitHub tools"}}}`
	resp := performCompatRequest(t, handler, http.MethodPut, "/api/mcp/config", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read extensions config: %v", err)
	}
	var cfg gatewayExtensionsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode extensions config: %v", err)
	}
	server, ok := cfg.MCPServers["github"]
	if !ok {
		t.Fatalf("missing github server in mcpServers: %s", string(data))
	}
	if !server.Enabled || server.Command != "npx" {
		t.Fatalf("persisted github server=%#v", server)
	}
}

func TestMCPConfigGetRefreshesExtensionsConfigCompatibilityFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "extensions_config.json")
	t.Setenv("DEERFLOW_EXTENSIONS_CONFIG_PATH", configPath)

	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "github": {
      "enabled": false,
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "description": "GitHub tools"
    }
  },
  "skills": {}
}`), 0o644); err != nil {
		t.Fatalf("write initial extensions config: %v", err)
	}

	_, handler := newCompatTestServer(t)

	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "github": {
      "enabled": true,
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "description": "GitHub tools refreshed"
    }
  },
  "skills": {}
}`), 0o644); err != nil {
		t.Fatalf("write updated extensions config: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/mcp/config", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var cfg gatewayMCPConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode mcp config: %v", err)
	}
	server, ok := cfg.MCPServers["github"]
	if !ok {
		t.Fatalf("missing github server after refresh: %#v", cfg.MCPServers)
	}
	if !server.Enabled || server.Description != "GitHub tools refreshed" {
		t.Fatalf("github server=%#v want enabled refreshed description", server)
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

func TestApplyGatewayMCPConfigDefersConnectedToolsWhenToolSearchEnabled(t *testing.T) {
	t.Setenv("DEERFLOW_TOOL_SEARCH_ENABLED", "true")

	s, _ := newCompatTestServer(t)
	s.tools = tools.NewRegistry()
	s.mcpConnector = func(ctx context.Context, name string, cfg gatewayMCPServerConfig) (gatewayMCPClient, error) {
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

	if tool := s.tools.Get("github.search_repos"); tool != nil {
		t.Fatal("expected MCP tool to stay out of the base registry when deferred")
	}
	deferred := s.currentDeferredMCPTools()
	if len(deferred) != 1 || deferred[0].Name != "github.search_repos" {
		t.Fatalf("deferred=%v", deferred)
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
			"supports_reasoning_effort": true,
			"supports_vision": true
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
	if !payload.Models[0].SupportsVision {
		t.Fatalf("expected explicit vision support for %#v", payload.Models[0])
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

func TestModelsEndpointSupportsConfiguredModelCatalogFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
models:
  - name: gpt-5
    model: openai/gpt-5
    display_name: GPT-5
    description: Primary reasoning model
    supports_thinking: true
    supports_reasoning_effort: true
    supports_vision: true
  - name: gemini-2.5-flash
    model: google/gemini-2.5-flash
    display_name: Gemini 2.5 Flash
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEERFLOW_CONFIG_PATH", configPath)

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
	if payload.Models[0].Name != "gpt-5" || payload.Models[1].Name != "gemini-2.5-flash" {
		t.Fatalf("unexpected models=%#v", payload.Models)
	}
	if !payload.Models[0].SupportsVision || !payload.Models[0].SupportsThinking || !payload.Models[0].SupportsReasoningEffort {
		t.Fatalf("missing explicit capabilities in %#v", payload.Models[0])
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

func TestSkillInstallUsesConfiguredSkillsPath(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, "config.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  path: ./configured-skills\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEERFLOW_CONFIG_PATH", configPath)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-config-root"
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

	target := filepath.Join(projectRoot, "configured-skills", "custom", "demo-skill", "SKILL.md")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected installed skill file under configured root: %v", err)
	}

	legacyTarget := filepath.Join(s.dataRoot, "skills", "custom", "demo-skill", "SKILL.md")
	if _, err := os.Stat(legacyTarget); !os.IsNotExist(err) {
		t.Fatalf("legacy install target should be unused, stat err=%v", err)
	}
}

func TestSkillInstallAcceptsArtifactURLs(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-artifact-url"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	archiveName := "demo skill.skill"
	archivePath := filepath.Join(uploadDir, archiveName)
	if err := writeSkillArchive(archivePath, "demo-skill"); err != nil {
		t.Fatalf("write skill archive: %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{
			name: "gateway artifact path",
			path: "/api/threads/" + threadID + "/artifacts/mnt/user-data/uploads/" + url.PathEscape(archiveName),
		},
		{
			name: "gateway artifact url with origin and query",
			path: "https://example.com/api/threads/" + threadID + "/artifacts/mnt/user-data/uploads/" + url.PathEscape(archiveName) + "?download=true",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"thread_id":"` + threadID + `","path":"` + tc.path + `"}`
			resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
			if resp.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			var payload struct {
				Success   bool   `json:"success"`
				SkillName string `json:"skill_name"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("decode install response: %v", err)
			}
			if !payload.Success || payload.SkillName != "demo-skill" {
				t.Fatalf("unexpected install payload: %#v", payload)
			}

			installed := filepath.Join(s.dataRoot, "skills", "custom", "demo-skill", "SKILL.md")
			if _, err := os.Stat(installed); err != nil {
				t.Fatalf("expected installed skill file: %v", err)
			}
			if err := os.RemoveAll(filepath.Dir(installed)); err != nil {
				t.Fatalf("cleanup installed skill dir: %v", err)
			}
		})
	}
}

func TestSkillInstallRejectsInvalidFrontmatter(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread-skill-invalid-frontmatter"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing frontmatter",
			content: "# Demo\n",
			want:    "no yaml frontmatter",
		},
		{
			name:    "unexpected key",
			content: "---\nname: demo-skill\ndescription: Demo skill\ncustom-field: true\n---\n# Demo\n",
			want:    "unexpected key",
		},
		{
			name:    "missing description",
			content: "---\nname: demo-skill\n---\n# Demo\n",
			want:    "missing description",
		},
		{
			name:    "invalid name",
			content: "---\nname: DemoSkill\ndescription: Demo skill\n---\n# Demo\n",
			want:    "hyphen-case",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archivePath := filepath.Join(uploadDir, strings.ReplaceAll(tc.name, " ", "-")+".skill")
			if err := writeRawSkillArchive(archivePath, tc.content); err != nil {
				t.Fatalf("write raw skill archive: %v", err)
			}

			body := `{"thread_id":"` + threadID + `","path":"/mnt/user-data/uploads/` + filepath.Base(archivePath) + `"}`
			resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			if !strings.Contains(strings.ToLower(resp.Body.String()), tc.want) {
				t.Fatalf("body=%q want substring %q", resp.Body.String(), tc.want)
			}
		})
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

func TestSkillEnableDisableAliasesMatchOriginalGateway(t *testing.T) {
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

	disableResp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/frontend-design/disable", nil, nil)
	if disableResp.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disableResp.Code, disableResp.Body.String())
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills/frontend-design", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	var skill gatewaySkill
	if err := json.NewDecoder(getResp.Body).Decode(&skill); err != nil {
		t.Fatalf("decode disabled skill: %v", err)
	}
	if skill.Enabled {
		t.Fatal("expected skill to be disabled")
	}

	enableResp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/frontend-design/enable", nil, nil)
	if enableResp.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", enableResp.Code, enableResp.Body.String())
	}

	getResp = performCompatRequest(t, handler, http.MethodGet, "/api/skills/frontend-design", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	if err := json.NewDecoder(getResp.Body).Decode(&skill); err != nil {
		t.Fatalf("decode enabled skill: %v", err)
	}
	if !skill.Enabled {
		t.Fatal("expected skill to be enabled")
	}
}

func TestSkillGetRejectsAmbiguousDuplicateNamesWithoutCategory(t *testing.T) {
	s, handler := newCompatTestServer(t)

	for _, rel := range []string{
		filepath.Join("skills", "public", "duplicate-skill", "SKILL.md"),
		filepath.Join("skills", "custom", "duplicate-skill", "SKILL.md"),
	} {
		path := filepath.Join(s.dataRoot, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		category := skillCategoryPublic
		if strings.Contains(rel, "/custom/") {
			category = skillCategoryCustom
		}
		if err := os.WriteFile(path, []byte(`---
name: duplicate-skill
description: `+category+` variant.
license: MIT
---
# Duplicate Skill
`), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/skills/duplicate-skill", nil, nil)
	if resp.Code != http.StatusConflict {
		t.Fatalf("get status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "specify category=public or category=custom") {
		t.Fatalf("body=%q missing ambiguity hint", resp.Body.String())
	}
}

func TestSkillSetEnabledRejectsAmbiguousDuplicateNamesWithoutCategory(t *testing.T) {
	s, handler := newCompatTestServer(t)

	writeSkill := func(category string) {
		path := filepath.Join(s.dataRoot, "skills", category, "duplicate-skill", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(`---
name: duplicate-skill
description: `+category+` variant.
license: MIT
---
# Duplicate Skill
`), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	writeSkill(skillCategoryPublic)
	writeSkill(skillCategoryCustom)

	ambiguousResp := performCompatRequest(t, handler, http.MethodPut, "/api/skills/duplicate-skill", strings.NewReader(`{"enabled":false}`), map[string]string{"Content-Type": "application/json"})
	if ambiguousResp.Code != http.StatusConflict {
		t.Fatalf("ambiguous update status=%d body=%s", ambiguousResp.Code, ambiguousResp.Body.String())
	}
	if !strings.Contains(ambiguousResp.Body.String(), "specify category=public or category=custom") {
		t.Fatalf("body=%q missing ambiguity hint", ambiguousResp.Body.String())
	}
}

func TestSkillUpdateReturnsSkillObjectLikeOriginalGateway(t *testing.T) {
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

	resp := performCompatRequest(t, handler, http.MethodPut, "/api/skills/frontend-design", strings.NewReader(`{"enabled":false}`), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode update payload: %v", err)
	}
	if got := asString(payload["name"]); got != "frontend-design" {
		t.Fatalf("name=%q want frontend-design", got)
	}
	if got, ok := payload["enabled"].(bool); !ok || got {
		t.Fatalf("enabled=%#v want false", payload["enabled"])
	}
	if _, exists := payload["skill"]; exists {
		t.Fatalf("unexpected wrapped skill payload: %#v", payload)
	}
	if _, exists := payload["success"]; exists {
		t.Fatalf("unexpected success wrapper payload: %#v", payload)
	}
}

func TestSkillToggleRollsBackOnPersistFailure(t *testing.T) {
	s, handler := newCompatTestServer(t)

	if err := os.MkdirAll(s.gatewayStatePath(), 0o755); err != nil {
		t.Fatalf("mkdir gateway state path: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/deep-research/disable", nil, nil)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("disable status=%d body=%s", resp.Code, resp.Body.String())
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills/deep-research", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}

	var skill gatewaySkill
	if err := json.NewDecoder(getResp.Body).Decode(&skill); err != nil {
		t.Fatalf("decode skill: %v", err)
	}
	if !skill.Enabled {
		t.Fatalf("skill=%#v want enabled after rollback", skill)
	}
}

func TestMCPConfigPutRollsBackOnPersistFailure(t *testing.T) {
	s, handler := newCompatTestServer(t)

	if err := os.MkdirAll(s.gatewayStatePath(), 0o755); err != nil {
		t.Fatalf("mkdir gateway state path: %v", err)
	}

	body := `{"mcp_servers":{"github":{"enabled":true,"type":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-github"],"env":{"GITHUB_TOKEN":"$GITHUB_TOKEN"},"description":"GitHub MCP server"}}}`
	resp := performCompatRequest(t, handler, http.MethodPut, "/api/mcp/config", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("put status=%d body=%s", resp.Code, resp.Body.String())
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/mcp/config", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}

	var cfg gatewayMCPConfig
	if err := json.NewDecoder(getResp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.MCPServers["github"].Enabled {
		t.Fatalf("github=%#v want disabled after rollback", cfg.MCPServers["github"])
	}
}

func TestUserProfilePutRollsBackOnPersistFailure(t *testing.T) {
	s, handler := newCompatTestServer(t)

	s.uiStateMu.Lock()
	s.setUserProfileLocked("before")
	s.uiStateMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.userProfilePath()), 0o755); err != nil {
		t.Fatalf("mkdir user profile dir: %v", err)
	}
	if err := os.WriteFile(s.userProfilePath(), []byte("before"), 0o644); err != nil {
		t.Fatalf("write initial user profile: %v", err)
	}
	if err := os.MkdirAll(s.gatewayStatePath(), 0o755); err != nil {
		t.Fatalf("mkdir gateway state path: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPut, "/api/user-profile", strings.NewReader(`{"content":"after"}`), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("put status=%d body=%s", resp.Code, resp.Body.String())
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/user-profile", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	var payload struct {
		Content *string `json:"content"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if payload.Content == nil || *payload.Content != "before" {
		t.Fatalf("content=%v want before", payload.Content)
	}

	data, err := os.ReadFile(s.userProfilePath())
	if err != nil {
		t.Fatalf("read user profile: %v", err)
	}
	if string(data) != "before" {
		t.Fatalf("user profile file=%q want before", string(data))
	}
}

func TestSkillInstallRollsBackOnPersistFailure(t *testing.T) {
	s, handler := newCompatTestServer(t)

	threadID := "thread-install-rollback"
	archiveName := "sample.skill"
	writeTestSkillArchive(t, filepath.Join(s.uploadsDir(threadID), archiveName))
	if err := os.MkdirAll(s.gatewayStatePath(), 0o755); err != nil {
		t.Fatalf("mkdir gateway state path: %v", err)
	}

	body := `{"thread_id":"` + threadID + `","path":"` + uploadArtifactURL(threadID, archiveName) + `"}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/skills/install", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("install status=%d body=%s", resp.Code, resp.Body.String())
	}

	if _, err := os.Stat(filepath.Join(s.gatewayCustomSkillsRoot(), "sample-skill")); !os.IsNotExist(err) {
		t.Fatalf("installed skill dir should be removed, stat err=%v", err)
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills/sample-skill?category=custom", nil, nil)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
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

func TestAgentEndpointsDiscoverFilesystemAgents(t *testing.T) {
	s, handler := newCompatTestServer(t)

	agentDir := s.agentDir("disk-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.yaml"), []byte("name: disk-agent\ndescription: Loaded from disk.\nmodel: gpt-5\ntool_groups:\n  - bash\n"), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("# Disk Agent\n\nUse filesystem-backed config."), 0o644); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listed struct {
		Agents []gatewayAgent `json:"agents"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Agents) != 1 || listed.Agents[0].Name != "disk-agent" {
		t.Fatalf("listed agents=%#v", listed.Agents)
	}
	if listed.Agents[0].Soul != "" {
		t.Fatalf("list should omit soul, got %#v", listed.Agents[0])
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents/disk-agent", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	var got gatewayAgent
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Description != "Loaded from disk." {
		t.Fatalf("description=%q want=%q", got.Description, "Loaded from disk.")
	}
	if !strings.Contains(got.Soul, "filesystem-backed config") {
		t.Fatalf("soul=%q", got.Soul)
	}

	checkResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents/check?name=disk-agent", nil, nil)
	if checkResp.Code != http.StatusOK {
		t.Fatalf("check status=%d body=%s", checkResp.Code, checkResp.Body.String())
	}
	var check struct {
		Available bool `json:"available"`
	}
	if err := json.NewDecoder(checkResp.Body).Decode(&check); err != nil {
		t.Fatalf("decode check response: %v", err)
	}
	if check.Available {
		t.Fatal("expected filesystem agent name to be unavailable")
	}

	updateResp := performCompatRequest(t, handler, http.MethodPut, "/api/agents/disk-agent", strings.NewReader(`{"description":"Updated from API.","soul":"# Disk Agent\n\nUpdated soul."}`), map[string]string{"Content-Type": "application/json"})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}
	assertFileContains(t, filepath.Join(agentDir, "config.yaml"), "description: Updated from API.")
	assertFileContains(t, filepath.Join(agentDir, "SOUL.md"), "Updated soul.")

	deleteResp := performCompatRequest(t, handler, http.MethodDelete, "/api/agents/disk-agent", nil, nil)
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	if _, err := os.Stat(agentDir); !os.IsNotExist(err) {
		t.Fatalf("agent dir still exists: %v", err)
	}
}

func TestRunsStreamAppliesFilesystemCustomAgentRuntimeConfig(t *testing.T) {
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
	s.ensureSession("thread-disk-agent", map[string]any{"title": "Existing title"})

	agentDir := s.agentDir("disk-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.yaml"), []byte("name: disk-agent\ndescription: Review code from disk.\nmodel: custom-model\ntool_groups:\n  - file:read\n"), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("# Disk Agent\n\nReview carefully."), 0o644); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	body := `{
		"thread_id":"thread-disk-agent",
		"input":{"messages":[{"role":"user","content":"Review this patch"}]},
		"context":{"agent_name":"disk-agent"}
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
	if !strings.Contains(provider.lastReq.SystemPrompt, "Review code from disk.") {
		t.Fatalf("system prompt missing description: %q", provider.lastReq.SystemPrompt)
	}
	if !strings.Contains(provider.lastReq.SystemPrompt, "Review carefully.") {
		t.Fatalf("system prompt missing soul: %q", provider.lastReq.SystemPrompt)
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
	if strings.Join(gotTools, ",") != "ask_clarification,bash,ls,present_files,read_file" {
		t.Fatalf("tools=%q want=%q", strings.Join(gotTools, ","), "ask_clarification,bash,ls,present_files,read_file")
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

func TestResolveRunConfigRefreshesUserProfileFromDisk(t *testing.T) {
	s, _ := newCompatTestServer(t)

	s.uiStateMu.Lock()
	s.setUserProfileLocked("stale profile")
	s.uiStateMu.Unlock()

	if err := os.WriteFile(s.userProfilePath(), []byte("Prefers refreshed disk profile."), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "Prefers refreshed disk profile.") {
		t.Fatalf("system prompt missing refreshed profile: %q", cfg.SystemPrompt)
	}
	if strings.Contains(cfg.SystemPrompt, "stale profile") {
		t.Fatalf("system prompt unexpectedly kept stale profile: %q", cfg.SystemPrompt)
	}
}

func TestRunsStreamMapsThinkingDisabledToMinimalReasoningEffort(t *testing.T) {
	provider := &streamSpyProvider{}
	t.Setenv("DEERFLOW_MODELS_JSON", `[{"name":"gpt-5","model":"openai/gpt-5","supports_thinking":true,"supports_reasoning_effort":true}]`)

	s := &Server{
		llmProvider:  provider,
		defaultModel: "gpt-5",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}
	s.ensureSession("thread-flash-mode", map[string]any{"title": "Existing title"})

	body := `{
		"thread_id":"thread-flash-mode",
		"input":{"messages":[{"role":"user","content":"Answer quickly"}]},
		"context":{"thinking_enabled":false}
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

	if provider.lastReq.ReasoningEffort != "minimal" {
		t.Fatalf("reasoning_effort=%q want=%q", provider.lastReq.ReasoningEffort, "minimal")
	}
}

func TestRunsStreamClearsReasoningEffortWhenModelDoesNotSupportIt(t *testing.T) {
	provider := &streamSpyProvider{}
	t.Setenv("DEERFLOW_MODELS_JSON", `[{"name":"fast-model","model":"acme/fast-model","supports_thinking":false,"supports_reasoning_effort":false}]`)

	s := &Server{
		llmProvider:  provider,
		defaultModel: "fast-model",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}
	s.ensureSession("thread-no-reasoning", map[string]any{"title": "Existing title"})

	body := `{
		"thread_id":"thread-no-reasoning",
		"input":{"messages":[{"role":"user","content":"Be brief"}]},
		"context":{"thinking_enabled":false,"reasoning_effort":"high"}
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

	if provider.lastReq.ReasoningEffort != "" {
		t.Fatalf("reasoning_effort=%q want empty", provider.lastReq.ReasoningEffort)
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

func TestRunsStreamPrioritizesMemoryFactsRelevantToCurrentConversation(t *testing.T) {
	provider := &streamSpyProvider{}
	now := time.Now().UTC()
	store := &fakeGatewayMemoryStore{
		docs: map[string]memory.Document{
			"thread-memory-relevance": {
				SessionID: "thread-memory-relevance",
				User: memory.UserMemory{
					WorkContext: "Maintains multiple long-lived projects.",
				},
				Facts: []memory.Fact{
					{
						ID:         "cooking",
						Content:    "Collects vintage cookware and recipe books.",
						Category:   "personal",
						Confidence: 0.99,
						CreatedAt:  now.Add(-2 * time.Hour),
						UpdatedAt:  now.Add(-2 * time.Hour),
					},
					{
						ID:         "gateway",
						Content:    "Owns deerflow-go gateway compatibility work.",
						Category:   "project",
						Confidence: 0.72,
						CreatedAt:  now.Add(-time.Hour),
						UpdatedAt:  now.Add(-time.Hour),
					},
				},
				Source:    "thread-memory-relevance",
				UpdatedAt: now,
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

	body := `{"thread_id":"thread-memory-relevance","input":{"messages":[{"role":"user","content":"Please help me debug deerflow-go gateway compatibility."}]}}`
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
	if !strings.Contains(provider.lastReq.SystemPrompt, "Owns deerflow-go gateway compatibility work.") {
		t.Fatalf("system prompt missing relevant fact: %q", provider.lastReq.SystemPrompt)
	}
	if strings.Contains(provider.lastReq.SystemPrompt, "Collects vintage cookware and recipe books.") {
		t.Fatalf("system prompt should trim unrelated fact under relevance ranking: %q", provider.lastReq.SystemPrompt)
	}
}

func TestRunsStreamUsesAgentScopedMemoryForCustomAgents(t *testing.T) {
	provider := &streamSpyProvider{}
	store := &fakeGatewayMemoryStore{
		docs: map[string]memory.Document{
			"agent:code-reviewer": {
				SessionID: "agent:code-reviewer",
				User: memory.UserMemory{
					WorkContext: "Reviews Go patches across repositories.",
				},
				Facts: []memory.Fact{{
					ID:        "fact-1",
					Content:   "Prefers terse review summaries.",
					Category:  "preference",
					CreatedAt: time.Now().Add(-time.Hour).UTC(),
					UpdatedAt: time.Now().Add(-time.Hour).UTC(),
				}},
				Source:    "agent:code-reviewer",
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
		agents: map[string]gatewayAgent{
			"code-reviewer": {
				Name:        "code-reviewer",
				Description: "Review code changes carefully.",
				Soul:        "You are a meticulous code reviewer.",
			},
		},
	}

	body := `{
		"thread_id":"thread-custom-agent-memory",
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
	if !strings.Contains(provider.lastReq.SystemPrompt, "Reviews Go patches across repositories.") {
		t.Fatalf("system prompt missing agent-scoped work context: %q", provider.lastReq.SystemPrompt)
	}
	if !strings.Contains(provider.lastReq.SystemPrompt, "Prefers terse review summaries.") {
		t.Fatalf("system prompt missing agent-scoped fact: %q", provider.lastReq.SystemPrompt)
	}
}

func TestRunsStreamPreservesPresentedArtifactsAcrossTurns(t *testing.T) {
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

	threadID := "thread-persist-artifacts"
	session := s.ensureSession(threadID, map[string]any{"title": "Artifact thread"})
	externalPath := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(externalPath, []byte("# Summary\n"), 0o644); err != nil {
		t.Fatalf("write external artifact: %v", err)
	}
	if err := session.PresentFiles.Register(tools.PresentFile{
		Path:       filepath.Clean(externalPath),
		SourcePath: externalPath,
	}); err != nil {
		t.Fatalf("register external artifact: %v", err)
	}

	for i := 0; i < 2; i++ {
		body := `{"thread_id":"` + threadID + `","input":{"messages":[{"role":"user","content":"Continue"}]}}`
		req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleRunsStream(rec, req)
		resp := rec.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			payload, _ := io.ReadAll(resp.Body)
			t.Fatalf("run %d status=%d body=%s", i+1, resp.StatusCode, string(payload))
		}
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state is nil")
	}
	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if !slices.Contains(artifacts, filepath.Clean(externalPath)) {
		t.Fatalf("artifacts=%#v missing external presented artifact", artifacts)
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
	if !strings.Contains(cfg.SystemPrompt, "<name>bootstrap</name>") {
		t.Fatalf("system prompt missing bootstrap skill: %q", cfg.SystemPrompt)
	}
	if strings.Contains(cfg.SystemPrompt, "<name>deep-research</name>") {
		t.Fatalf("bootstrap prompt should not expose unrelated skills: %q", cfg.SystemPrompt)
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

func TestMemoryPutReplacesStoredDocument(t *testing.T) {
	store := &fakeGatewayMemoryStore{
		docs: map[string]memory.Document{
			"thread-memory-api": {
				SessionID: "thread-memory-api",
				User: memory.UserMemory{
					TopOfMind: "Old memory.",
				},
				Source:    "thread-memory-api",
				UpdatedAt: time.Now().Add(-time.Hour).UTC(),
			},
		},
	}
	s, handler := newCompatTestServer(t)
	s.memoryStore = store
	s.memorySvc = memory.NewService(store, fakeMemoryExtractor{})
	s.memoryThread = "thread-memory-api"

	body := `{
		"version":"1.0",
		"lastUpdated":"2026-04-02T10:30:00Z",
		"user":{
			"workContext":{"summary":"Owns the gateway integration.","updatedAt":"2026-04-02T09:00:00Z"},
			"personalContext":{"summary":"Prefers terse updates.","updatedAt":"2026-04-02T09:30:00Z"},
			"topOfMind":{"summary":"Ship memory editing.","updatedAt":"2026-04-02T10:00:00Z"}
		},
		"history":{
			"recentMonths":{"summary":"Focused on UI compatibility.","updatedAt":"2026-04-01T08:00:00Z"},
			"earlierContext":{"summary":"Migrated Python behaviors to Go.","updatedAt":"2026-03-01T08:00:00Z"},
			"longTermBackground":{"summary":"Maintains DeerFlow-related tooling.","updatedAt":"2025-12-01T08:00:00Z"}
		},
		"facts":[
			{
				"id":"fact-keep",
				"content":"User prioritizes UX regressions first.",
				"category":"preference",
				"confidence":0.9,
				"createdAt":"2026-04-02T10:15:00Z",
				"source":"thread-memory-api"
			},
			{
				"content":"Missing IDs are auto-generated.",
				"category":"note",
				"confidence":0.6
			}
		]
	}`

	putResp := performCompatRequest(t, handler, http.MethodPut, "/api/memory", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if putResp.Code != http.StatusOK {
		t.Fatalf("put memory status=%d body=%s", putResp.Code, putResp.Body.String())
	}

	doc, err := store.Load(context.Background(), "thread-memory-api")
	if err != nil {
		t.Fatalf("reload store doc: %v", err)
	}
	if doc.User.WorkContext != "Owns the gateway integration." {
		t.Fatalf("work context=%q", doc.User.WorkContext)
	}
	if doc.History.LongTermBackground != "Maintains DeerFlow-related tooling." {
		t.Fatalf("longTermBackground=%q", doc.History.LongTermBackground)
	}
	if len(doc.Facts) != 2 {
		t.Fatalf("facts=%d want=2", len(doc.Facts))
	}
	if doc.Facts[0].ID != "fact-keep" {
		t.Fatalf("first fact id=%q", doc.Facts[0].ID)
	}
	if doc.Facts[1].ID != "fact-2" {
		t.Fatalf("second fact id=%q want=%q", doc.Facts[1].ID, "fact-2")
	}
	if doc.UpdatedAt.Format(time.RFC3339) != "2026-04-02T10:30:00Z" {
		t.Fatalf("updatedAt=%s", doc.UpdatedAt.Format(time.RFC3339))
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/memory", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get memory after put status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), "Ship memory editing.") {
		t.Fatalf("memory body=%q", getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), "fact-2") {
		t.Fatalf("memory body missing generated fact id: %q", getResp.Body.String())
	}
}

func TestMemoryPutRejectsInvalidJSON(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPut, "/api/memory", strings.NewReader("{"), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid request body") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestGatewayMemoryToDocumentDerivesTimestampsAndSkipsBlankFacts(t *testing.T) {
	resp := gatewayMemoryResponse{
		User: memoryUser{
			TopOfMind: memorySection{
				Summary:   "Focus on memory editing.",
				UpdatedAt: "2026-04-02T09:00:00Z",
			},
		},
		History: memoryHistory{
			RecentMonths: memorySection{
				Summary:   "Recently ported gateway APIs to Go.",
				UpdatedAt: "2026-04-02T10:00:00Z",
			},
		},
		Facts: []memoryFact{
			{
				Content:   "Valid facts remain in the document.",
				CreatedAt: "2026-04-02T11:00:00Z",
			},
			{
				ID:      "blank-fact",
				Content: "   ",
			},
		},
	}

	doc := gatewayMemoryToDocument(resp, "thread-derived-memory")
	if doc.SessionID != "thread-derived-memory" {
		t.Fatalf("sessionID=%q", doc.SessionID)
	}
	if doc.UpdatedAt.Format(time.RFC3339) != "2026-04-02T11:00:00Z" {
		t.Fatalf("updatedAt=%s want=%s", doc.UpdatedAt.Format(time.RFC3339), "2026-04-02T11:00:00Z")
	}
	if len(doc.Facts) != 1 {
		t.Fatalf("facts=%d want=1", len(doc.Facts))
	}
	if doc.Facts[0].UpdatedAt.Format(time.RFC3339) != "2026-04-02T11:00:00Z" {
		t.Fatalf("fact updatedAt=%s", doc.Facts[0].UpdatedAt.Format(time.RFC3339))
	}
}

func TestRunsStreamPersistsMemoryUpdatesToAgentScope(t *testing.T) {
	provider := &streamSpyProvider{}
	store := &fakeGatewayMemoryStore{docs: map[string]memory.Document{}}
	s := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		runStreams:   make(map[string]map[uint64]chan StreamEvent),
		dataRoot:     t.TempDir(),
		memoryStore:  store,
		memorySvc:    memory.NewService(store, fakeStaticMemoryExtractor{update: memory.Update{User: memory.UserMemory{TopOfMind: "Track review follow-ups."}}}),
		agents: map[string]gatewayAgent{
			"code-reviewer": {
				Name:        "code-reviewer",
				Description: "Review code changes carefully.",
			},
		},
	}

	body := `{
		"thread_id":"thread-custom-agent-memory-save",
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
	s.waitForBackgroundTasks()

	doc, ok := store.docs["agent:code-reviewer"]
	if !ok {
		t.Fatalf("expected agent-scoped memory document, got keys=%v", mapsKeys(store.docs))
	}
	if doc.User.TopOfMind != "Track review follow-ups" {
		t.Fatalf("top_of_mind=%q want=%q", doc.User.TopOfMind, "Track review follow-ups")
	}
	if _, ok := store.docs["thread-custom-agent-memory-save"]; ok {
		t.Fatalf("unexpected thread-scoped memory document: %#v", store.docs["thread-custom-agent-memory-save"])
	}
}

func TestAgentScopedMemoryUpdateDoesNotReplaceGlobalMemoryEndpoint(t *testing.T) {
	provider := &streamSpyProvider{}
	store := &fakeGatewayMemoryStore{
		docs: map[string]memory.Document{
			"thread-global-memory": {
				SessionID: "thread-global-memory",
				Source:    "thread-global-memory",
				User: memory.UserMemory{
					TopOfMind: "Global memory should stay visible.",
				},
				UpdatedAt: time.Now().Add(-time.Minute).UTC(),
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
		memorySvc:    memory.NewService(store, fakeStaticMemoryExtractor{update: memory.Update{User: memory.UserMemory{TopOfMind: "Agent private memory."}}}),
		memoryThread: "thread-global-memory",
		memory: gatewayMemoryResponse{
			LastUpdated: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			User: memoryUser{
				TopOfMind: memorySection{Summary: "Global memory should stay visible."},
			},
		},
		agents: map[string]gatewayAgent{
			"code-reviewer": {
				Name:        "code-reviewer",
				Description: "Review code changes carefully.",
			},
		},
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	body := `{
		"thread_id":"thread-custom-agent-memory-cache",
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
	s.waitForBackgroundTasks()

	memResp := performCompatRequest(t, mux, http.MethodGet, "/api/memory", nil, nil)
	if memResp.Code != http.StatusOK {
		t.Fatalf("memory status=%d body=%s", memResp.Code, memResp.Body.String())
	}
	if !strings.Contains(memResp.Body.String(), "Global memory should stay visible.") {
		t.Fatalf("memory body=%q", memResp.Body.String())
	}
	if strings.Contains(memResp.Body.String(), "Agent private memory.") {
		t.Fatalf("agent memory leaked into global endpoint: %q", memResp.Body.String())
	}
	if got := s.memoryThread; got != "thread-global-memory" {
		t.Fatalf("memoryThread=%q want thread-global-memory", got)
	}
	if doc, ok := store.docs["agent:code-reviewer"]; !ok {
		t.Fatalf("expected agent-scoped memory document, got keys=%v", mapsKeys(store.docs))
	} else if doc.User.TopOfMind != "Agent private memory" {
		t.Fatalf("agent top_of_mind=%q want=%q", doc.User.TopOfMind, "Agent private memory")
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
		{Name: "ls", Groups: []string{"builtin", "file_ops"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "present_file", Groups: []string{"builtin", "file_ops"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "ask_clarification", Groups: []string{"builtin", "interaction"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
		{Name: "str_replace", Groups: []string{"builtin", "file_ops"}, Handler: func(context.Context, models.ToolCall) (models.ToolResult, error) { return models.ToolResult{}, nil }},
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

func (f *fakeGatewayMemoryStore) Delete(_ context.Context, sessionID string) error {
	if f.docs == nil {
		return memory.ErrNotFound
	}
	if _, ok := f.docs[sessionID]; !ok {
		return memory.ErrNotFound
	}
	delete(f.docs, sessionID)
	return nil
}

type fakeMemoryExtractor struct{}

func (fakeMemoryExtractor) ExtractUpdate(context.Context, memory.Document, []models.Message) (memory.Update, error) {
	return memory.Update{}, nil
}

type fakeStaticMemoryExtractor struct {
	update memory.Update
}

func (f fakeStaticMemoryExtractor) ExtractUpdate(context.Context, memory.Document, []models.Message) (memory.Update, error) {
	return f.update, nil
}

func mapsKeys[K comparable, V any](in map[K]V) []K {
	out := make([]K, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	return out
}

func writeSkillArchive(path, name string) error {
	content := "---\nname: " + name + "\ndescription: Demo skill\ncategory: productivity\nlicense: MIT\n---\n# Demo\n"
	return writeRawSkillArchive(path, content)
}

func writeRawSkillArchive(path, content string) error {
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
	if _, err := w.Write([]byte(content)); err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}
