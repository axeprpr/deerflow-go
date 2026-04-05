package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type fakeLLMProvider struct{}

func (fakeLLMProvider) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (fakeLLMProvider) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 2)
	go func() {
		defer close(ch)
		ch <- llm.StreamChunk{Delta: "hello from fake llm"}
		ch <- llm.StreamChunk{
			Done: true,
			Message: &models.Message{
				ID:        "ai-final",
				SessionID: "session",
				Role:      models.RoleAI,
				Content:   "hello from fake llm",
			},
		}
	}()
	return ch, nil
}

type fakeToolLLMProvider struct {
	mu    sync.Mutex
	calls int
}

func (*fakeToolLLMProvider) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (p *fakeToolLLMProvider) Stream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	p.mu.Lock()
	p.calls++
	callIndex := p.calls
	p.mu.Unlock()

	ch := make(chan llm.StreamChunk, 2)
	go func() {
		defer close(ch)
		if callIndex == 1 {
			ch <- llm.StreamChunk{
				ToolCalls: []models.ToolCall{
					{
						ID:        "call-1",
						Name:      "read_file",
						Arguments: map[string]any{"path": "/tmp/demo.txt"},
						Status:    models.CallStatusPending,
					},
				},
			}
			ch <- llm.StreamChunk{
				Done: true,
				Message: &models.Message{
					ID:        "ai-final-tool",
					SessionID: "session",
					Role:      models.RoleAI,
					ToolCalls: []models.ToolCall{
						{
							ID:        "call-1",
							Name:      "read_file",
							Arguments: map[string]any{"path": "/tmp/demo.txt"},
							Status:    models.CallStatusPending,
						},
					},
				},
			}
			return
		}
		ch <- llm.StreamChunk{Delta: "done"}
		ch <- llm.StreamChunk{
			Done: true,
			Message: &models.Message{
				ID:        "ai-final-answer",
				SessionID: "session",
				Role:      models.RoleAI,
				Content:   "done",
			},
			Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		}
	}()
	return ch, nil
}

func writeGatewaySkill(t *testing.T, root, category, name, frontmatter string) {
	t.Helper()
	skillDir := filepath.Join(root, "skills", category, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(frontmatter), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func newCompatTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	root := t.TempDir()
	writeGatewaySkill(t, root, "public", "deep-research", "---\nname: deep-research\ndescription: Research and summarize a topic with structured outputs.\ncategory: public\nlicense: MIT\n---\n")
	s := &Server{
		sessions:  make(map[string]*Session),
		runs:      make(map[string]*Run),
		dataRoot:  root,
		startedAt: time.Now().UTC(),
		models:    defaultGatewayModels("qwen/Qwen3.5-9B"),
		skills:    nil,
		mcpConfig: defaultGatewayMCPConfig(),
		agents:    map[string]gatewayAgent{},
		memory:    defaultGatewayMemory(),
	}
	s.clarify = clarification.NewManager(8)
	s.clarifyAPI = clarification.NewAPI(s.clarify)
	s.skills = s.discoverGatewaySkills(nil)
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, ts
}

func TestAPILangGraphPrefixCreateThread(t *testing.T) {
	_, ts := newCompatTestServer(t)
	resp, err := http.Post(ts.URL+"/api/langgraph/threads", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(body))
	}
}

func TestUploadsAndArtifactsEndpoints(t *testing.T) {
	s, ts := newCompatTestServer(t)
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

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/threads/"+threadID+"/uploads", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status=%d body=%s", resp.StatusCode, string(b))
	}

	listResp, err := http.Get(ts.URL + "/api/threads/" + threadID + "/uploads/list")
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", listResp.StatusCode)
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

	artifactURL := ts.URL + "/api/threads/" + threadID + "/artifacts/mnt/user-data/uploads/hello.txt"
	artifactResp, err := http.Get(artifactURL)
	if err != nil {
		t.Fatalf("artifact request: %v", err)
	}
	defer artifactResp.Body.Close()
	if artifactResp.StatusCode != http.StatusOK {
		t.Fatalf("artifact status=%d", artifactResp.StatusCode)
	}
	artifactBody, _ := io.ReadAll(artifactResp.Body)
	if string(artifactBody) != "hello artifact" {
		t.Fatalf("artifact body=%q", string(artifactBody))
	}
}

func TestSuggestionsEndpoint(t *testing.T) {
	_, ts := newCompatTestServer(t)
	payload := `{"messages":[{"role":"user","content":"请帮我分析部署方案"}],"n":3}`
	resp, err := http.Post(ts.URL+"/api/threads/t1/suggestions", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("suggestions request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
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

func TestGatewayThreadDeleteRemovesLocalData(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-delete-1"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(uploadDir, "a.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/threads/"+threadID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err=%v", err)
	}
}

func TestModelsSkillsMCPConfigEndpoints(t *testing.T) {
	_, ts := newCompatTestServer(t)

	modelsResp, err := http.Get(ts.URL + "/api/models")
	if err != nil {
		t.Fatalf("models request: %v", err)
	}
	defer modelsResp.Body.Close()
	if modelsResp.StatusCode != http.StatusOK {
		t.Fatalf("models status=%d", modelsResp.StatusCode)
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
	if got := modelsData.Models[0]["name"]; got != "qwen/Qwen3.5-9B" {
		t.Fatalf("first model=%v want qwen/Qwen3.5-9B", got)
	}

	skillsResp, err := http.Get(ts.URL + "/api/skills")
	if err != nil {
		t.Fatalf("skills request: %v", err)
	}
	defer skillsResp.Body.Close()
	if skillsResp.StatusCode != http.StatusOK {
		t.Fatalf("skills status=%d", skillsResp.StatusCode)
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

	setBody := strings.NewReader(`{"enabled":false}`)
	setReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/skills/deep-research", setBody)
	setReq.Header.Set("Content-Type", "application/json")
	setResp, err := http.DefaultClient.Do(setReq)
	if err != nil {
		t.Fatalf("set skill request: %v", err)
	}
	defer setResp.Body.Close()
	if setResp.StatusCode != http.StatusOK {
		t.Fatalf("set skill status=%d", setResp.StatusCode)
	}

	mcpResp, err := http.Get(ts.URL + "/api/mcp/config")
	if err != nil {
		t.Fatalf("mcp get request: %v", err)
	}
	defer mcpResp.Body.Close()
	if mcpResp.StatusCode != http.StatusOK {
		t.Fatalf("mcp get status=%d", mcpResp.StatusCode)
	}

	putMCPReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/mcp/config", strings.NewReader(`{"mcp_servers":{"foo":{"enabled":true,"description":"x"}}}`))
	putMCPReq.Header.Set("Content-Type", "application/json")
	putMCPResp, err := http.DefaultClient.Do(putMCPReq)
	if err != nil {
		t.Fatalf("mcp put request: %v", err)
	}
	defer putMCPResp.Body.Close()
	if putMCPResp.StatusCode != http.StatusOK {
		t.Fatalf("mcp put status=%d", putMCPResp.StatusCode)
	}

	modelGetResp, err := http.Get(ts.URL + "/api/models/qwen/Qwen3.5-9B")
	if err != nil {
		t.Fatalf("model get request: %v", err)
	}
	defer modelGetResp.Body.Close()
	if modelGetResp.StatusCode != http.StatusOK {
		t.Fatalf("model get status=%d", modelGetResp.StatusCode)
	}
}

func TestGatewayModelStatePersistence(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		dataRoot:     root,
		defaultModel: "qwen/Qwen3.5-9B",
		models: map[string]gatewayModel{
			"custom-model": {
				Name:                    "custom-model",
				Model:                   "provider/custom-model",
				DisplayName:             "Custom Model",
				SupportsThinking:        true,
				SupportsReasoningEffort: true,
			},
		},
		skills:    map[string]gatewaySkill{},
		mcpConfig: defaultGatewayMCPConfig(),
		agents:    map[string]gatewayAgent{},
		memory:    defaultGatewayMemory(),
	}

	if err := s.persistGatewayState(); err != nil {
		t.Fatalf("persist state: %v", err)
	}

	loaded := &Server{
		dataRoot:     root,
		defaultModel: "qwen/Qwen3.5-9B",
		models:       map[string]gatewayModel{},
		skills:       map[string]gatewaySkill{},
		mcpConfig:    defaultGatewayMCPConfig(),
		agents:       map[string]gatewayAgent{},
		memory:       defaultGatewayMemory(),
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("load state: %v", err)
	}

	loaded.uiStateMu.RLock()
	custom, ok := loaded.findModelLocked("custom-model")
	defaultModel, hasDefault := loaded.findModelLocked("qwen/Qwen3.5-9B")
	loaded.uiStateMu.RUnlock()
	if !ok {
		t.Fatal("expected custom model after reload")
	}
	if custom.DisplayName != "Custom Model" {
		t.Fatalf("display_name=%q want Custom Model", custom.DisplayName)
	}
	if !hasDefault {
		t.Fatal("expected default model to be preserved after reload")
	}
	if defaultModel.Name != "qwen/Qwen3.5-9B" {
		t.Fatalf("default model name=%q", defaultModel.Name)
	}
}

func TestFindModelLockedSupportsProviderName(t *testing.T) {
	s := &Server{
		defaultModel: "qwen/Qwen3.5-9B",
		models: map[string]gatewayModel{
			"custom-model": {
				ID:          "custom-model",
				Name:        "custom-model",
				Model:       "provider/custom-model",
				DisplayName: "Custom Model",
			},
		},
	}

	model, ok := s.findModelLocked("provider/custom-model")
	if !ok {
		t.Fatal("expected lookup by provider model name to succeed")
	}
	if model.Name != "custom-model" {
		t.Fatalf("name=%q want custom-model", model.Name)
	}
}

func TestGatewayModelsFromEnvJSON(t *testing.T) {
	models := gatewayModelsFromEnv(`[
		{"name":"flash","model":"qwen/Qwen3.5-9B","display_name":"Flash","supports_thinking":false},
		{"name":"pro","model":"deepseek/deepseek-r1","display_name":"Pro","supports_thinking":true,"supports_reasoning_effort":true}
	]`)
	if len(models) != 2 {
		t.Fatalf("len=%d want 2", len(models))
	}
	if models[0].Name != "flash" || models[0].DisplayName != "Flash" {
		t.Fatalf("unexpected first model: %#v", models[0])
	}
	if !models[1].SupportsThinking || !models[1].SupportsReasoningEffort {
		t.Fatalf("unexpected second model capabilities: %#v", models[1])
	}
}

func TestGatewayModelsFromEnvCSV(t *testing.T) {
	models := gatewayModelsFromEnv("qwen/Qwen3.5-9B, deepseek/deepseek-r1 , ,")
	if len(models) != 2 {
		t.Fatalf("len=%d want 2", len(models))
	}
	if models[0].Name != "qwen/Qwen3.5-9B" {
		t.Fatalf("first=%q", models[0].Name)
	}
	if models[1].Name != "deepseek/deepseek-r1" {
		t.Fatalf("second=%q", models[1].Name)
	}
}

func TestGatewayMCPConfigFromEnv(t *testing.T) {
	cfg := gatewayMCPConfigFromEnv(`{
		"mcp_servers": {
			"github": {
				"enabled": true,
				"type": "http",
				"url": "https://example.com/mcp",
				"headers": {"Authorization":"Bearer $TOKEN"},
				"oauth": {"enabled": true, "token_url": "https://example.com/token"}
			}
		}
	}`)
	server, ok := cfg.MCPServers["github"]
	if !ok {
		t.Fatal("expected github server")
	}
	if server.Type != "http" || server.URL != "https://example.com/mcp" {
		t.Fatalf("unexpected mcp server: %#v", server)
	}
	if server.OAuth == nil || server.OAuth.TokenField != "access_token" {
		t.Fatalf("unexpected oauth defaults: %#v", server.OAuth)
	}
}

func TestAgentFilesPersistAndLoad(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	model := "qwen/Qwen3.5-9B"
	agent := gatewayAgent{
		Name:        "researcher",
		Description: "Research agent",
		Model:       &model,
		ToolGroups:  []string{"web", "file"},
		Soul:        "Be precise.",
	}
	if err := s.persistAgentFiles("researcher", agent); err != nil {
		t.Fatalf("persistAgentFiles: %v", err)
	}
	loaded := s.loadAgentsFromFiles()
	got, ok := loaded["researcher"]
	if !ok {
		t.Fatal("expected persisted agent")
	}
	if got.Description != "Research agent" || got.Soul != "Be precise." {
		t.Fatalf("unexpected agent: %#v", got)
	}
}

func TestUserProfileFileLoad(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	if err := os.WriteFile(s.userProfilePath(), []byte("user profile"), 0o644); err != nil {
		t.Fatalf("write user profile: %v", err)
	}
	content, ok := s.loadUserProfileFromFile()
	if !ok || content != "user profile" {
		t.Fatalf("content=%q ok=%v", content, ok)
	}
}

func TestMemoryFilePersistAndLoad(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		dataRoot: root,
		memory: gatewayMemoryResponse{
			Version:     "1.0",
			LastUpdated: "2026-01-01T00:00:00Z",
			Facts: []memoryFact{{
				ID:        "fact-1",
				Content:   "prefers sqlite",
				Category:  "preference",
				CreatedAt: "2026-01-01T00:00:00Z",
				Source:    "thread-1",
			}},
		},
	}
	if err := s.persistMemoryFile(); err != nil {
		t.Fatalf("persistMemoryFile: %v", err)
	}
	mem, ok := s.loadMemoryFromFile()
	if !ok {
		t.Fatal("expected memory from file")
	}
	if len(mem.Facts) != 1 || mem.Facts[0].ID != "fact-1" {
		t.Fatalf("unexpected memory: %#v", mem)
	}
}

func TestAgentEndpointsPersistFiles(t *testing.T) {
	s, ts := newCompatTestServer(t)
	body := `{"name":"writer","description":"Writes clearly","model":"qwen/Qwen3.5-9B","tool_groups":["file"],"soul":"Write with structure."}`
	resp, err := http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", resp.StatusCode)
	}

	if _, err := os.Stat(filepath.Join(s.agentDir("writer"), "config.json")); err != nil {
		t.Fatalf("expected config.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.agentDir("writer"), "SOUL.md")); err != nil {
		t.Fatalf("expected SOUL.md: %v", err)
	}

	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/agents/writer", nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d", delResp.StatusCode)
	}
	if _, err := os.Stat(s.agentDir("writer")); !os.IsNotExist(err) {
		t.Fatalf("expected agent dir removed, err=%v", err)
	}
}

func TestDefaultGatewayMemoryConfigFromEnv(t *testing.T) {
	t.Setenv("DEERFLOW_MEMORY_CONFIG", `{"enabled":true,"debounce_seconds":10,"max_facts":55,"fact_confidence_threshold":0.9,"injection_enabled":false,"max_injection_tokens":999}`)
	cfg := defaultGatewayMemoryConfig("/tmp/deerflow-test")
	if cfg.StoragePath != "/tmp/deerflow-test/memory.json" {
		t.Fatalf("storage_path=%q", cfg.StoragePath)
	}
	if cfg.DebounceSeconds != 10 || cfg.MaxFacts != 55 {
		t.Fatalf("unexpected cfg: %#v", cfg)
	}
	if cfg.InjectionEnabled {
		t.Fatalf("expected injection disabled: %#v", cfg)
	}
}

func TestDefaultGatewayMemoryConfigPathOverride(t *testing.T) {
	t.Setenv("DEERFLOW_MEMORY_PATH", "/custom/memory.json")
	cfg := defaultGatewayMemoryConfig("/tmp/deerflow-test")
	if cfg.StoragePath != "/custom/memory.json" {
		t.Fatalf("storage_path=%q want /custom/memory.json", cfg.StoragePath)
	}
}

func TestDefaultGatewayChannelsStatusFromEnv(t *testing.T) {
	t.Setenv("DEERFLOW_CHANNELS_CONFIG", `{"service_running":true,"channels":{"telegram":{"enabled":true,"connected":false}}}`)
	status := defaultGatewayChannelsStatus()
	if !status.ServiceRunning {
		t.Fatalf("expected service running: %#v", status)
	}
	if _, ok := status.Channels["telegram"]; !ok {
		t.Fatalf("expected telegram channel: %#v", status)
	}
}

func TestPersistedThreadsAndRunsLoad(t *testing.T) {
	root := t.TempDir()
	s := &Server{
		dataRoot:  root,
		sessions:  map[string]*Session{},
		runs:      map[string]*Run{},
		startedAt: time.Now().UTC(),
	}
	session := &Session{
		ThreadID:  "thread-1",
		Messages:  []models.Message{{ID: "m1", Role: models.RoleHuman, Content: "hello"}},
		Metadata:  map[string]any{"title": "Test Thread"},
		Status:    "idle",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.persistSessionFile(session); err != nil {
		t.Fatalf("persistSessionFile: %v", err)
	}
	run := &Run{
		RunID:       "run-1",
		ThreadID:    "thread-1",
		AssistantID: "agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events:      []StreamEvent{{ID: "1", Event: "end", Data: map[string]any{"ok": true}}},
	}
	if err := s.persistRunFile(run); err != nil {
		t.Fatalf("persistRunFile: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		sessions: map[string]*Session{},
		runs:     map[string]*Run{},
	}
	loaded.loadPersistedThreads()
	loaded.loadPersistedRuns()
	if _, ok := loaded.sessions["thread-1"]; !ok {
		t.Fatal("expected thread restored")
	}
	gotRun, ok := loaded.runs["run-1"]
	if !ok {
		t.Fatal("expected run restored")
	}
	if len(gotRun.Events) != 1 || gotRun.Events[0].Event != "end" {
		t.Fatalf("unexpected run events: %#v", gotRun.Events)
	}
}

func TestThreadCreatePersistsFile(t *testing.T) {
	s, ts := newCompatTestServer(t)
	resp, err := http.Post(ts.URL+"/threads", "application/json", strings.NewReader(`{"thread_id":"persist-me","metadata":{"title":"Persisted"}}`))
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if _, err := os.Stat(s.threadStatePath("persist-me")); err != nil {
		t.Fatalf("expected thread.json persisted: %v", err)
	}
}

func TestThreadHistorySnapshots(t *testing.T) {
	_, ts := newCompatTestServer(t)
	resp, err := http.Post(ts.URL+"/threads", "application/json", strings.NewReader(`{"thread_id":"history-me"}`))
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/history-me/state", strings.NewReader(`{"values":{"title":"Version 1"}}`))
	req.Header.Set("Content-Type", "application/json")
	stateResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update state: %v", err)
	}
	stateResp.Body.Close()

	historyReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/history-me/history", strings.NewReader(`{"limit":5}`))
	historyResp, err := http.DefaultClient.Do(historyReq)
	if err != nil {
		t.Fatalf("history request: %v", err)
	}
	defer historyResp.Body.Close()
	var history []ThreadState
	if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("expected non-empty history")
	}

	getResp, err := http.Get(ts.URL + "/threads/history-me/history")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get history status=%d", getResp.StatusCode)
	}
}

func TestThreadDeleteRemovesRunFiles(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-cleanup"
	run := &Run{
		RunID:       "run-cleanup",
		ThreadID:    threadID,
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/threads/"+threadID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if _, err := os.Stat(s.runStatePath("run-cleanup")); !os.IsNotExist(err) {
		t.Fatalf("expected run file removed, err=%v", err)
	}
}

func TestThreadRunsListAndScopedGet(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-list-1",
		ThreadID:    "thread-list-1",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	listResp, err := http.Get(ts.URL + "/threads/thread-list-1/runs")
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	defer listResp.Body.Close()
	var listData struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listData); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(listData.Runs) != 1 || listData.Runs[0]["run_id"] != "run-list-1" {
		t.Fatalf("unexpected runs payload: %#v", listData.Runs)
	}

	getResp, err := http.Get(ts.URL + "/threads/thread-list-1/runs/run-list-1")
	if err != nil {
		t.Fatalf("get scoped run: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", getResp.StatusCode)
	}
}

func TestThreadRunsCreateReturnsFinalState(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]}}`
	resp, err := http.Post(ts.URL+"/threads/thread-sync-run/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["thread_id"] != "thread-sync-run" {
		t.Fatalf("thread_id=%v", payload["thread_id"])
	}
	if payload["run_id"] == "" {
		t.Fatalf("run_id missing: %#v", payload)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("messages missing: %#v", payload)
	}
}

func TestThreadClarificationsList(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-clarify", nil)

	createBody := `{"question":"Need more detail?","required":true}`
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-clarify/clarifications", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create clarification: %v", err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", createResp.StatusCode)
	}

	listResp, err := http.Get(ts.URL + "/threads/thread-clarify/clarifications")
	if err != nil {
		t.Fatalf("list clarifications: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", listResp.StatusCode)
	}
	var payload struct {
		Clarifications []map[string]any `json:"clarifications"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Clarifications) != 1 {
		t.Fatalf("clarifications=%d", len(payload.Clarifications))
	}
}

func TestThreadClarificationsListIsStable(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-clarify-order", nil)

	for _, body := range []string{
		`{"question":"First?","required":true}`,
		`{"question":"Second?","required":true}`,
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-clarify-order/clarifications", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("create clarification: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create status=%d", resp.StatusCode)
		}
	}

	resp, err := http.Get(ts.URL + "/threads/thread-clarify-order/clarifications")
	if err != nil {
		t.Fatalf("list clarifications: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Clarifications []map[string]any `json:"clarifications"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Clarifications) != 2 {
		t.Fatalf("clarifications=%d", len(payload.Clarifications))
	}
	if payload.Clarifications[0]["question"] != "First?" || payload.Clarifications[1]["question"] != "Second?" {
		t.Fatalf("clarifications=%#v", payload.Clarifications)
	}
}

func TestThreadStateIncludesCompatFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-fields"
	s.ensureSession(threadID, map[string]any{
		"title": "Compat Thread",
		"todos": []any{
			map[string]any{"content": "first", "status": "pending"},
		},
		"sandbox": map[string]any{
			"sandbox_id": "sb-1",
		},
		"viewed_images": map[string]any{
			"/tmp/example.png": map[string]any{
				"base64":    "abc",
				"mime_type": "image/png",
			},
		},
	})
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.txt"), []byte("brief"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	resp, err := http.Get(ts.URL + "/threads/" + threadID + "/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var state ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}

	threadData, ok := state.Values["thread_data"].(map[string]any)
	if !ok {
		t.Fatalf("thread_data missing: %#v", state.Values)
	}
	if threadData["uploads_path"] != s.uploadsDir(threadID) {
		t.Fatalf("uploads_path=%v want %s", threadData["uploads_path"], s.uploadsDir(threadID))
	}

	todos, ok := state.Values["todos"].([]any)
	if !ok || len(todos) != 1 {
		t.Fatalf("todos=%#v", state.Values["todos"])
	}

	uploadedFiles, ok := state.Values["uploaded_files"].([]any)
	if !ok || len(uploadedFiles) != 1 {
		t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
	}

	sandboxState, ok := state.Values["sandbox"].(map[string]any)
	if !ok || sandboxState["sandbox_id"] != "sb-1" {
		t.Fatalf("sandbox=%#v", state.Values["sandbox"])
	}
}

func TestThreadStatePostPersistsCompatFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-post"
	s.ensureSession(threadID, nil)

	body := `{"values":{"title":"Updated","todos":[{"content":"ship sqlite","status":"in_progress"}],"sandbox":{"sandbox_id":"sb-2"},"viewed_images":{"/tmp/chart.png":{"base64":"xyz","mime_type":"image/png"}}}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/"+threadID+"/state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post state: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	s.sessionsMu.RLock()
	session := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if session == nil {
		t.Fatal("session missing")
	}
	if session.Metadata["title"] != "Updated" {
		t.Fatalf("title=%v", session.Metadata["title"])
	}

	todos, ok := session.Metadata["todos"].([]map[string]any)
	if !ok || len(todos) != 1 || todos[0]["content"] != "ship sqlite" {
		t.Fatalf("todos=%#v", session.Metadata["todos"])
	}

	sandboxState, ok := session.Metadata["sandbox"].(map[string]any)
	if !ok || sandboxState["sandbox_id"] != "sb-2" {
		t.Fatalf("sandbox=%#v", session.Metadata["sandbox"])
	}

	viewedImages, ok := session.Metadata["viewed_images"].(map[string]any)
	if !ok || len(viewedImages) != 1 {
		t.Fatalf("viewed_images=%#v", session.Metadata["viewed_images"])
	}
}

func TestThreadRunPersistsConfigMetadata(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]},"config":{"configurable":{"model_name":"deepseek/deepseek-r1","thinking_enabled":false,"is_plan_mode":true,"subagent_enabled":true,"reasoning_effort":"high","agent_type":"deep_research"}}}`
	resp, err := http.Post(ts.URL+"/threads/thread-config-run/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	threadResp, err := http.Get(ts.URL + "/threads/thread-config-run")
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	defer threadResp.Body.Close()
	if threadResp.StatusCode != http.StatusOK {
		t.Fatalf("thread status=%d", threadResp.StatusCode)
	}
	var thread map[string]any
	if err := json.NewDecoder(threadResp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	metadata, ok := thread["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%#v", thread["metadata"])
	}
	if metadata["model_name"] != "deepseek/deepseek-r1" {
		t.Fatalf("model_name=%v", metadata["model_name"])
	}
	if metadata["thinking_enabled"] != false {
		t.Fatalf("thinking_enabled=%v", metadata["thinking_enabled"])
	}
	if metadata["is_plan_mode"] != true || metadata["subagent_enabled"] != true {
		t.Fatalf("metadata=%#v", metadata)
	}

	stateResp, err := http.Get(ts.URL + "/threads/thread-config-run/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer stateResp.Body.Close()
	var state ThreadState
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	config, ok := state.Config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", state.Config)
	}
	if config["model_name"] != "deepseek/deepseek-r1" {
		t.Fatalf("config=%#v", config)
	}
	if config["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort=%v", config["reasoning_effort"])
	}
	if config["thinking_enabled"] != false || config["is_plan_mode"] != true || config["subagent_enabled"] != true {
		t.Fatalf("config=%#v", config)
	}
}

func TestThreadRunAcceptsTopLevelContext(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]},"context":{"model_name":"qwen/Qwen3.5-9B","thinking_enabled":false,"is_plan_mode":true,"subagent_enabled":true,"reasoning_effort":"medium"}}`
	resp, err := http.Post(ts.URL+"/threads/thread-context-run/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	stateResp, err := http.Get(ts.URL + "/threads/thread-context-run/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer stateResp.Body.Close()
	var state ThreadState
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	config, ok := state.Config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", state.Config)
	}
	if config["model_name"] != "qwen/Qwen3.5-9B" || config["reasoning_effort"] != "medium" {
		t.Fatalf("config=%#v", config)
	}
	if config["thinking_enabled"] != false || config["is_plan_mode"] != true || config["subagent_enabled"] != true {
		t.Fatalf("config=%#v", config)
	}
}

func TestThreadRunPersistsCoreMetadataFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]}}`
	resp, err := http.Post(ts.URL+"/threads/thread-core-meta/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	threadResp, err := http.Get(ts.URL + "/threads/thread-core-meta")
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	defer threadResp.Body.Close()
	var thread map[string]any
	if err := json.NewDecoder(threadResp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	metadata, ok := thread["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%#v", thread["metadata"])
	}
	if metadata["thread_id"] != "thread-core-meta" {
		t.Fatalf("thread_id=%v", metadata["thread_id"])
	}
	if metadata["assistant_id"] != "lead_agent" || metadata["graph_id"] != "lead_agent" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["run_id"] == "" {
		t.Fatalf("run_id missing: %#v", metadata)
	}
}

func TestConvertToMessagesInjectsUploadedFilesContext(t *testing.T) {
	s, _ := newCompatTestServer(t)
	threadID := "thread-upload-context"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "existing.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing upload: %v", err)
	}

	input := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "Please read the docs"},
			},
			"additional_kwargs": map[string]any{
				"files": []any{
					map[string]any{
						"filename": "brief.md",
						"size":     2048,
						"path":     "/mnt/user-data/uploads/brief.md",
					},
				},
			},
		},
	}

	messages := s.convertToMessages(threadID, input)
	if len(messages) != 1 {
		t.Fatalf("messages=%d", len(messages))
	}
	content := messages[0].Content
	if !strings.Contains(content, "<uploaded_files>") {
		t.Fatalf("content missing upload block: %q", content)
	}
	if !strings.Contains(content, "brief.md") || !strings.Contains(content, "existing.txt") {
		t.Fatalf("content missing file names: %q", content)
	}
	if !strings.Contains(content, "Please read the docs") {
		t.Fatalf("content missing original text: %q", content)
	}
}

func TestMessagesToLangChainPreservesToolShape(t *testing.T) {
	s, _ := newCompatTestServer(t)
	converted := s.messagesToLangChain([]models.Message{
		{
			ID:        "ai-1",
			SessionID: "thread-1",
			Role:      models.RoleAI,
			Content:   "",
			ToolCalls: []models.ToolCall{
				{
					ID:        "call-1",
					Name:      "present_files",
					Arguments: map[string]any{"filepaths": []string{"/tmp/report.md"}},
					Status:    models.CallStatusCompleted,
				},
			},
			Metadata: map[string]string{
				"stop_reason": "tool_calls",
			},
		},
		{
			ID:        "tool-1",
			SessionID: "thread-1",
			Role:      models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "call-1",
				ToolName: "present_files",
				Status:   models.CallStatusCompleted,
				Content:  "presented",
				Data: map[string]any{
					"files": []string{"/tmp/report.md"},
				},
			},
		},
	})
	if len(converted) != 2 {
		t.Fatalf("messages=%d", len(converted))
	}
	if len(converted[0].ToolCalls) != 1 || converted[0].ToolCalls[0].Name != "present_files" {
		t.Fatalf("tool calls=%#v", converted[0].ToolCalls)
	}
	if converted[0].AdditionalKwargs["stop_reason"] != "tool_calls" {
		t.Fatalf("additional_kwargs=%#v", converted[0].AdditionalKwargs)
	}
	if converted[1].ToolCallID != "call-1" || converted[1].Name != "present_files" {
		t.Fatalf("tool message=%#v", converted[1])
	}
	data, ok := converted[1].Data["data"].(map[string]any)
	if !ok || len(data) != 1 {
		t.Fatalf("tool data=%#v", converted[1].Data)
	}
	if converted[0].ToolCalls[0].Args["filepaths"] == nil {
		t.Fatalf("normalized present_file args=%#v", converted[0].ToolCalls[0].Args)
	}
}

func TestThreadArtifactsRecoveredFromMessageHistory(t *testing.T) {
	s, _ := newCompatTestServer(t)
	session := s.ensureSession("thread-artifacts", map[string]any{"title": "Artifacts"})
	session.Messages = []models.Message{
		{
			ID:        "ai-1",
			SessionID: "thread-artifacts",
			Role:      models.RoleAI,
			ToolCalls: []models.ToolCall{
				{
					ID:        "call-1",
					Name:      "present_file",
					Arguments: map[string]any{"path": "/tmp/report.md"},
					Status:    models.CallStatusCompleted,
				},
			},
		},
		{
			ID:        "tool-1",
			SessionID: "thread-artifacts",
			Role:      models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "call-2",
				ToolName: "present_files",
				Status:   models.CallStatusCompleted,
				Data: map[string]any{
					"filepaths": []any{"/tmp/slides.html"},
				},
			},
		},
	}

	state := s.getThreadState("thread-artifacts")
	if state == nil {
		t.Fatal("state missing")
	}
	artifacts, ok := state.Values["artifacts"].([]string)
	if !ok {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifacts=%#v", artifacts)
	}
	if artifacts[0] != "/tmp/report.md" || artifacts[1] != "/tmp/slides.html" {
		t.Fatalf("artifacts=%#v", artifacts)
	}
}

func TestThreadRunStreamEmitsToolEndAliasAndUsageMetadata(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = &fakeToolLLMProvider{}
	if err := os.WriteFile("/tmp/demo.txt", []byte("demo"), 0o644); err != nil {
		t.Fatalf("write tool fixture: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-events/runs/stream", strings.NewReader(`{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"inspect file"}]}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: on_tool_end") {
		t.Fatalf("missing on_tool_end event: %s", text)
	}
	if !strings.Contains(text, "\"usage_metadata\":{\"input_tokens\":10,\"output_tokens\":5,\"total_tokens\":15}") {
		t.Fatalf("missing usage_metadata in messages-tuple: %s", text)
	}
	if !strings.Contains(text, "\"type\":\"tool\",\"id\":\"tool:call-1\"") {
		t.Fatalf("missing stable tool message id: %s", text)
	}
}

func TestThreadRunStreamModeValuesFiltersMessageEvents(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	reqBody := `{"assistant_id":"lead_agent","stream_mode":["values"],"input":{"messages":[{"role":"user","content":"hello"}]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-values/runs/stream", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: values") {
		t.Fatalf("missing values event: %s", text)
	}
	if strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("unexpected messages-tuple event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
}

func TestThreadRunStreamModeMessagesTupleFiltersValues(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	reqBody := `{"assistant_id":"lead_agent","stream_mode":["messages-tuple"],"input":{"messages":[{"role":"user","content":"hello"}]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-messages/runs/stream", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
}

func TestThreadRunCamelCaseStreamModeFiltersValues(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	reqBody := `{"assistant_id":"lead_agent","streamMode":["messages-tuple"],"input":{"messages":[{"role":"user","content":"hello"}]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-camel/runs/stream", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
}

func TestStreamModeFilterAliases(t *testing.T) {
	filter := newStreamModeFilter([]any{"tasks", "messages"})
	if !filter.allows("task_started") || !filter.allows("task_completed") {
		t.Fatalf("tasks mode should allow task lifecycle events: %#v", filter)
	}
	if !filter.allows("chunk") || !filter.allows("messages-tuple") {
		t.Fatalf("messages mode should allow chunk and messages-tuple")
	}
	if filter.allows("values") {
		t.Fatal("values should not be allowed when not requested")
	}
	if !filter.allows("end") {
		t.Fatal("end should always be allowed")
	}
}

func TestRecordedRunStreamModeFiltersReplayEvents(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-1",
		ThreadID:    "thread-replay-1",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "metadata", Data: map[string]any{"run_id": "run-replay-1"}},
			{ID: "2", Event: "messages-tuple", Data: map[string]any{"type": "ai", "content": "hello"}},
			{ID: "3", Event: "values", Data: map[string]any{"title": "done"}},
			{ID: "4", Event: "end", Data: map[string]any{"run_id": "run-replay-1"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-1/runs/run-replay-1/stream?stream_mode=values")
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: values") {
		t.Fatalf("missing values event: %s", text)
	}
	if strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("unexpected messages-tuple event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
}

func TestThreadJoinStreamModeFiltersReplayEvents(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-1",
		ThreadID:    "thread-join-1",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "ai", "content": "hello"}},
			{ID: "2", Event: "values", Data: map[string]any{"title": "done"}},
			{ID: "3", Event: "end", Data: map[string]any{"run_id": "run-join-1"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-1/stream?streamMode=messages-tuple")
	if err != nil {
		t.Fatalf("join stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
}

func TestSkillInstallFromArchive(t *testing.T) {
	s, ts := newCompatTestServer(t)
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
	resp, err := http.Post(ts.URL+"/api/skills/install", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("install request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
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
	if payload.Skill["category"] != "productivity" {
		t.Fatalf("skill category=%v want productivity", payload.Skill["category"])
	}
	if payload.Skill["license"] != "MIT" {
		t.Fatalf("skill license=%v want MIT", payload.Skill["license"])
	}

	target := filepath.Join(s.dataRoot, "skills", "custom", "demo-skill", "SKILL.md")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected installed skill file: %v", err)
	}
}

func TestAgentsAndMemoryEndpoints(t *testing.T) {
	_, ts := newCompatTestServer(t)

	createBody := `{"name":"my-agent","description":"a","model":"qwen/Qwen3.5-9B","tool_groups":["file"],"soul":"hello"}`
	createResp, err := http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("create agent request: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent status=%d", createResp.StatusCode)
	}

	listResp, err := http.Get(ts.URL + "/api/agents")
	if err != nil {
		t.Fatalf("list agents request: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list agents status=%d", listResp.StatusCode)
	}

	getResp, err := http.Get(ts.URL + "/api/agents/my-agent")
	if err != nil {
		t.Fatalf("get agent request: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get agent status=%d", getResp.StatusCode)
	}

	checkResp, err := http.Get(ts.URL + "/api/agents/check?name=my-agent")
	if err != nil {
		t.Fatalf("check agent request: %v", err)
	}
	defer checkResp.Body.Close()
	if checkResp.StatusCode != http.StatusOK {
		t.Fatalf("check agent status=%d", checkResp.StatusCode)
	}

	memResp, err := http.Get(ts.URL + "/api/memory")
	if err != nil {
		t.Fatalf("memory request: %v", err)
	}
	defer memResp.Body.Close()
	if memResp.StatusCode != http.StatusOK {
		t.Fatalf("memory status=%d", memResp.StatusCode)
	}

	memCfgResp, err := http.Get(ts.URL + "/api/memory/config")
	if err != nil {
		t.Fatalf("memory config request: %v", err)
	}
	defer memCfgResp.Body.Close()
	if memCfgResp.StatusCode != http.StatusOK {
		t.Fatalf("memory config status=%d", memCfgResp.StatusCode)
	}

	chResp, err := http.Get(ts.URL + "/api/channels")
	if err != nil {
		t.Fatalf("channels request: %v", err)
	}
	defer chResp.Body.Close()
	if chResp.StatusCode != http.StatusOK {
		t.Fatalf("channels status=%d", chResp.StatusCode)
	}
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
