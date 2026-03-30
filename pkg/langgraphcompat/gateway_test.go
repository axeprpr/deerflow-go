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

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func newCompatTestServer(t *testing.T) (*Server, http.Handler) {
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

func TestAPILangGraphPrefixCreateThread(t *testing.T) {
	_, handler := newCompatTestServer(t)
	resp := performCompatRequest(t, handler, http.MethodPost, "/api/langgraph/threads", strings.NewReader(`{}`), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
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

	artifactResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/hello.txt", nil, nil)
	if artifactResp.Code != http.StatusOK {
		t.Fatalf("artifact status=%d", artifactResp.Code)
	}
	if artifactResp.Body.String() != "hello artifact" {
		t.Fatalf("artifact body=%q", artifactResp.Body.String())
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

	modelGetResp := performCompatRequest(t, handler, http.MethodGet, "/api/models/qwen/Qwen3.5-9B", nil, nil)
	if modelGetResp.Code != http.StatusOK {
		t.Fatalf("model get status=%d", modelGetResp.Code)
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
	if payload.Models[1].Name != "gpt-5" {
		t.Fatalf("unexpected models ordering/content: %#v", payload.Models)
	}
	if payload.Models[1].Model != "openai/gpt-5" {
		t.Fatalf("model=%q want=%q", payload.Models[1].Model, "openai/gpt-5")
	}
	if !payload.Models[1].SupportsThinking {
		t.Fatalf("expected explicit thinking support for %#v", payload.Models[1])
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
	if payload.Models[0].Name != "claude-3-7-sonnet" {
		t.Fatalf("unexpected first model: %#v", payload.Models[0])
	}
	if payload.Models[0].DisplayName != "claude-3-7-sonnet" {
		t.Fatalf("display_name=%q want=%q", payload.Models[0].DisplayName, "claude-3-7-sonnet")
	}
	if !payload.Models[0].SupportsReasoningEffort {
		t.Fatalf("expected reasoning support for %#v", payload.Models[0])
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
