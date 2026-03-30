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
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func newCompatTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	root := t.TempDir()
	s := &Server{
		sessions:  make(map[string]*Session),
		runs:      make(map[string]*Run),
		dataRoot:  root,
		startedAt: time.Now().UTC(),
		skills:    defaultGatewaySkills(),
		mcpConfig: defaultGatewayMCPConfig(),
		agents:    map[string]gatewayAgent{},
		memory:    defaultGatewayMemory(),
	}
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

func TestUploadConvertibleDocumentCreatesMarkdownCompanion(t *testing.T) {
	_, ts := newCompatTestServer(t)
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

	mdResp, err := http.Get(ts.URL + "/api/threads/" + threadID + "/artifacts/mnt/user-data/uploads/report.md")
	if err != nil {
		t.Fatalf("markdown artifact request: %v", err)
	}
	defer mdResp.Body.Close()
	if mdResp.StatusCode != http.StatusOK {
		t.Fatalf("markdown artifact status=%d", mdResp.StatusCode)
	}
	mdBody, _ := io.ReadAll(mdResp.Body)
	if !strings.Contains(string(mdBody), "Quarterly Review") {
		t.Fatalf("markdown body=%q missing extracted text", string(mdBody))
	}
}

func TestDeleteConvertibleUploadRemovesMarkdownCompanion(t *testing.T) {
	s, ts := newCompatTestServer(t)
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

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/threads/"+threadID+"/uploads/report.docx", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("expected original removed, stat err=%v", err)
	}
	if _, err := os.Stat(companion); !os.IsNotExist(err) {
		t.Fatalf("expected companion removed, stat err=%v", err)
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

func TestSuggestionsEndpointUsesLLMResponse(t *testing.T) {
	provider := &titleProvider{response: "```json\n[\"Q1\",\"Q2\",\"Q3\"]\n```"}
	s, ts := newCompatTestServer(t)
	s.llmProvider = provider
	s.defaultModel = "default-model"

	payload := `{"messages":[{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello"}],"n":2,"model_name":"run-model"}`
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
