package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

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
