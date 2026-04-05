package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/axeprpr/deerflow-go/pkg/tools"
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

type fakeErrorLLMProvider struct{}

func (fakeErrorLLMProvider) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (fakeErrorLLMProvider) Stream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- llm.StreamChunk{Err: fmt.Errorf("boom")}
	}()
	return ch, nil
}

type fakeClarificationLLMProvider struct {
	mu    sync.Mutex
	calls int
}

func (*fakeClarificationLLMProvider) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (p *fakeClarificationLLMProvider) Stream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
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
						ID:   "clarify-1",
						Name: "ask_clarification",
						Arguments: map[string]any{
							"type":     "text",
							"question": "Need more detail?",
							"required": true,
						},
						Status: models.CallStatusPending,
					},
				},
			}
			ch <- llm.StreamChunk{
				Done: true,
				Message: &models.Message{
					ID:        "ai-final-clarify",
					SessionID: "session",
					Role:      models.RoleAI,
					ToolCalls: []models.ToolCall{
						{
							ID:   "clarify-1",
							Name: "ask_clarification",
							Arguments: map[string]any{
								"type":     "text",
								"question": "Need more detail?",
								"required": true,
							},
							Status: models.CallStatusPending,
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
				ID:        "ai-final-clarification-answer",
				SessionID: "session",
				Role:      models.RoleAI,
				Content:   "done",
			},
		}
	}()
	return ch, nil
}

func sseEventBlock(t *testing.T, text string, event string) string {
	t.Helper()
	marker := "event: " + event + "\n"
	start := strings.Index(text, marker)
	if start < 0 {
		t.Fatalf("missing %s event in %s", event, text)
	}
	block := text[start:]
	if next := strings.Index(block, "\n\nid: "); next >= 0 {
		block = block[:next]
	}
	return block
}

func sseEventBlocks(text string, event string) []string {
	marker := "event: " + event + "\n"
	if !strings.Contains(text, marker) {
		return nil
	}
	parts := strings.Split(text, marker)
	blocks := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		block := marker + part
		if next := strings.Index(block, "\n\nid: "); next >= 0 {
			block = block[:next]
		}
		blocks = append(blocks, block)
	}
	return blocks
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

func TestCreateThreadAcceptsCamelCaseThreadID(t *testing.T) {
	_, ts := newCompatTestServer(t)
	resp, err := http.Post(ts.URL+"/threads", "application/json", strings.NewReader(`{"threadId":"thread-camel-create"}`))
	if err != nil {
		t.Fatalf("post thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(body))
	}
	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	if thread["thread_id"] != "thread-camel-create" {
		t.Fatalf("thread_id=%v", thread["thread_id"])
	}
}

func TestCreateThreadAcceptsValuesPayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	resp, err := http.Post(
		ts.URL+"/threads",
		"application/json",
		strings.NewReader(`{"thread_id":"thread-create-values","values":{"title":"Created","todos":[{"content":"ship sqlite","status":"pending"}]}}`),
	)
	if err != nil {
		t.Fatalf("post thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(body))
	}

	state := s.getThreadState("thread-create-values")
	if state == nil {
		t.Fatal("state missing")
	}
	if got := state.Values["title"]; got != "Created" {
		t.Fatalf("title=%v want Created", got)
	}
	todos, ok := state.Values["todos"].([]map[string]any)
	if !ok || len(todos) != 1 {
		t.Fatalf("todos=%#v", state.Values["todos"])
	}
}

func TestCreateThreadAcceptsTopLevelValues(t *testing.T) {
	s, ts := newCompatTestServer(t)
	resp, err := http.Post(
		ts.URL+"/threads",
		"application/json",
		strings.NewReader(`{"threadId":"thread-create-top-level","metadata":{"assistantId":"assistant-1","graphId":"graph-1","runId":"run-1","mode":"thinking","modelName":"deepseek/deepseek-r1","reasoningEffort":"high","agentName":"planner","agentType":"research","thinkingEnabled":false,"isPlanMode":true,"subagentEnabled":true,"Temperature":0.2,"maxTokens":321,"checkpointId":"cp-1","parentCheckpointId":"cp-parent-1","checkpointNs":"ns-1","parentCheckpointNs":"ns-parent-1","checkpointThreadId":"checkpoint-thread-1","parentCheckpointThreadId":"checkpoint-thread-parent-1"},"title":"Created Top","todos":[{"content":"ship sqlite","status":"pending"}],"sandbox":{"sandbox_id":"sb-1"},"threadData":{"workspace_path":"/tmp/workspace","uploads_path":"/tmp/uploads","outputs_path":"/tmp/outputs"},"uploadedFiles":[{"filename":"notes.txt","path":"/tmp/uploads/notes.txt","status":"uploaded"}],"artifacts":["/tmp/report.html"],"viewedImages":{"/tmp/chart.png":{"base64":"xyz","mime_type":"image/png"}}}`),
	)
	if err != nil {
		t.Fatalf("post thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(body))
	}

	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	if thread["title"] != "Created Top" {
		t.Fatalf("thread=%#v", thread)
	}
	values, _ := thread["values"].(map[string]any)
	if got := values["title"]; got != "Created Top" {
		t.Fatalf("title=%v want Created Top", got)
	}
	viewedImages, ok := values["viewed_images"].(map[string]any)
	if !ok || len(viewedImages) != 1 {
		t.Fatalf("viewed_images=%#v", values["viewed_images"])
	}
	viewedImagesSummary, ok := thread["viewed_images"].(map[string]any)
	if !ok || len(viewedImagesSummary) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	todosSummary, _ := thread["todos"].([]any)
	if len(todosSummary) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	sandboxSummary, _ := thread["sandbox"].(map[string]any)
	if sandboxSummary["sandbox_id"] != "sb-1" {
		t.Fatalf("thread=%#v", thread)
	}
	threadDataSummary, _ := thread["thread_data"].(map[string]any)
	if threadDataSummary["workspace_path"] != "/tmp/workspace" || threadDataSummary["uploads_path"] != "/tmp/uploads" || threadDataSummary["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("thread=%#v", thread)
	}
	uploadedFilesSummary, _ := thread["uploaded_files"].([]any)
	if len(uploadedFilesSummary) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	artifactsSummary, _ := thread["artifacts"].([]any)
	if len(artifactsSummary) != 1 || artifactsSummary[0] != "/tmp/report.html" {
		t.Fatalf("thread=%#v", thread)
	}
	todosValues, _ := values["todos"].([]any)
	if len(todosValues) != 1 {
		t.Fatalf("values=%#v", values)
	}
	sandboxValues, _ := values["sandbox"].(map[string]any)
	if sandboxValues["sandbox_id"] != "sb-1" {
		t.Fatalf("values=%#v", values)
	}
	threadDataValues, _ := values["thread_data"].(map[string]any)
	if threadDataValues["workspace_path"] != "/tmp/workspace" || threadDataValues["uploads_path"] != "/tmp/uploads" || threadDataValues["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("values=%#v", values)
	}
	uploadedFilesValues, _ := values["uploaded_files"].([]any)
	if len(uploadedFilesValues) != 1 {
		t.Fatalf("values=%#v", values)
	}
	artifactsValues, _ := values["artifacts"].([]any)
	if len(artifactsValues) != 1 || artifactsValues[0] != "/tmp/report.html" {
		t.Fatalf("values=%#v", values)
	}
	config, _ := thread["config"].(map[string]any)
	configurable, _ := config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" || configurable["model_name"] != "deepseek/deepseek-r1" || configurable["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", config)
	}
	if configurable["agent_name"] != "planner" || configurable["agent_type"] != "research" {
		t.Fatalf("config=%#v", config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v", config)
	}
	metadata, _ := thread["metadata"].(map[string]any)
	if metadata["checkpoint_id"] != "cp-1" || metadata["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["checkpoint_ns"] != "ns-1" || metadata["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["checkpoint_thread_id"] != "checkpoint-thread-1" || metadata["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["assistant_id"] != "assistant-1" || metadata["graph_id"] != "graph-1" || metadata["run_id"] != "run-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["agent_name"] != "planner" || metadata["agent_type"] != "research" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if thread["assistant_id"] != "assistant-1" || thread["graph_id"] != "graph-1" || thread["run_id"] != "run-1" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["checkpoint_id"] != "cp-1" || thread["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("thread=%#v", thread)
	}
	checkpoint, _ := thread["checkpoint"].(map[string]any)
	if checkpoint["checkpoint_id"] != "cp-1" || checkpoint["checkpoint_ns"] != "ns-1" || checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", thread["checkpoint"])
	}
	parentCheckpoint, _ := thread["parent_checkpoint"].(map[string]any)
	if parentCheckpoint["checkpoint_id"] != "cp-parent-1" || parentCheckpoint["checkpoint_ns"] != "ns-parent-1" || parentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", thread["parent_checkpoint"])
	}

	state := s.getThreadState("thread-create-top-level")
	if state == nil {
		t.Fatal("state missing")
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

func TestSuggestionsEndpointAcceptsCountAlias(t *testing.T) {
	_, ts := newCompatTestServer(t)
	payload := `{"messages":[{"role":"user","content":"请帮我分析部署方案"}],"count":2,"modelName":"qwen/Qwen3.5-9B"}`
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
	if len(data.Suggestions) != 2 {
		t.Fatalf("len=%d want 2", len(data.Suggestions))
	}
}

func TestSuggestionsEndpointHonorsExplicitZeroN(t *testing.T) {
	_, ts := newCompatTestServer(t)
	payload := `{"messages":[{"role":"user","content":"请帮我分析部署方案"}],"n":0}`
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
	if len(data.Suggestions) != 0 {
		t.Fatalf("len=%d want 0", len(data.Suggestions))
	}
}

func TestSuggestionsEndpointHonorsExplicitZeroCount(t *testing.T) {
	_, ts := newCompatTestServer(t)
	payload := `{"messages":[{"role":"user","content":"请帮我分析部署方案"}],"count":0}`
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
	if len(data.Suggestions) != 0 {
		t.Fatalf("len=%d want 0", len(data.Suggestions))
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

func TestThreadSearchSelectProjectsFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	session := s.ensureSession("thread-search-select", map[string]any{"title": "Projected"})
	session.Messages = []models.Message{{
		ID:        "m1",
		SessionID: "thread-search-select",
		Role:      models.RoleHuman,
		Content:   "hello",
	}}

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["thread_id","updated_at","values"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-select" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	if _, ok := selected["values"]; !ok {
		t.Fatalf("missing values in %#v", selected)
	}
	if _, ok := selected["updated_at"]; !ok {
		t.Fatalf("missing updated_at in %#v", selected)
	}
	if _, ok := selected["created_at"]; !ok {
		t.Fatalf("missing created_at in %#v", selected)
	}
	if _, ok := selected["metadata"]; ok {
		t.Fatalf("unexpected metadata in %#v", selected)
	}
	if _, ok := selected["status"]; ok {
		t.Fatalf("unexpected status in %#v", selected)
	}
}

func TestThreadSearchSelectProjectsMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	session := s.ensureSession("thread-search-messages-select", map[string]any{"title": "Projected"})
	session.Messages = []models.Message{
		{
			ID:        "ai-1",
			SessionID: "thread-search-messages-select",
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
				"reasoning_content":   "Need to inspect report",
				"usage_input_tokens":  "10",
				"usage_output_tokens": "5",
				"usage_total_tokens":  "15",
			},
		},
		{
			ID:        "tool-1",
			SessionID: "thread-search-messages-select",
			Role:      models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "call-1",
				ToolName: "present_files",
				Status:   models.CallStatusCompleted,
				Content:  "presented",
				Duration: 1500 * time.Millisecond,
				Data: map[string]any{
					"files": []string{"/tmp/report.md"},
				},
			},
			Metadata: map[string]string{
				"message_status": "success",
			},
		},
	}

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["threadId","values"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-messages-select" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	values, _ := selected["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("values=%#v", values)
	}
	first, _ := messages[0].(map[string]any)
	if first["id"] != "ai-1" {
		t.Fatalf("message=%#v", first)
	}
	toolCalls, _ := first["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("message=%#v", first)
	}
	additionalKwargs, _ := first["additional_kwargs"].(map[string]any)
	if additionalKwargs["reasoning_content"] != "Need to inspect report" {
		t.Fatalf("additional_kwargs=%#v", additionalKwargs)
	}
	usage, _ := first["usage_metadata"].(map[string]any)
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) || usage["total_tokens"] != float64(15) {
		t.Fatalf("usage_metadata=%#v", usage)
	}
	second, _ := messages[1].(map[string]any)
	if second["id"] != "tool-1" || second["tool_call_id"] != "call-1" || second["status"] != "success" {
		t.Fatalf("message=%#v", second)
	}
}

func TestThreadSearchDefaultsToFiftyResults(t *testing.T) {
	s, ts := newCompatTestServer(t)
	for i := 0; i < 12; i++ {
		s.ensureSession(fmt.Sprintf("thread-search-default-%02d", i), nil)
	}

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 12 {
		t.Fatalf("len=%d want at least 12", len(threads))
	}
}

func TestThreadSearchHonorsExplicitZeroLimit(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-zero-limit", nil)

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":0}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) != 0 {
		t.Fatalf("len=%d want 0", len(threads))
	}
}

func TestThreadSearchHonorsExplicitZeroPageSize(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-zero-pagesize", nil)

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"pageSize":0}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) != 0 {
		t.Fatalf("len=%d want 0", len(threads))
	}
}

func TestThreadSearchClampsNegativeOffset(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-negative-offset", nil)

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":1,"offset":-5}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("len=%d want 1", len(threads))
	}
}

func TestThreadHistoryClampsNegativeLimit(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-history-negative-limit"
	session := s.ensureSession(threadID, map[string]any{"title": "History"})
	if err := s.persistSessionFile(session); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	if err := s.appendThreadHistorySnapshot(threadID); err != nil {
		t.Fatalf("append history: %v", err)
	}

	resp, err := http.Post(
		ts.URL+"/threads/"+threadID+"/history",
		"application/json",
		strings.NewReader(`{"limit":-5}`),
	)
	if err != nil {
		t.Fatalf("history request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var history []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("len=%d want 0", len(history))
	}
}

func TestThreadHistoryHonorsExplicitZeroLimit(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-history-zero-limit"
	session := s.ensureSession(threadID, map[string]any{"title": "History"})
	if err := s.persistSessionFile(session); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	if err := s.appendThreadHistorySnapshot(threadID); err != nil {
		t.Fatalf("append history: %v", err)
	}

	resp, err := http.Post(
		ts.URL+"/threads/"+threadID+"/history",
		"application/json",
		strings.NewReader(`{"limit":0}`),
	)
	if err != nil {
		t.Fatalf("history request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var history []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("len=%d want 0", len(history))
	}
}

func TestThreadSearchSelectStillSortsByUnselectedField(t *testing.T) {
	s, ts := newCompatTestServer(t)
	oldSession := s.ensureSession("thread-search-old", map[string]any{"title": "Old"})
	newSession := s.ensureSession("thread-search-new", map[string]any{"title": "New"})
	oldSession.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	newSession.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sort_by":"updated_at","sort_order":"desc","select":["thread_id"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-new" {
		t.Fatalf("first thread=%v want thread-search-new", threads[0]["thread_id"])
	}
	if _, ok := threads[0]["updated_at"]; !ok {
		t.Fatalf("missing updated_at in projected result: %#v", threads[0])
	}
}

func TestThreadSearchAcceptsCamelCaseSortFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	oldSession := s.ensureSession("thread-search-camel-old", map[string]any{"title": "Old"})
	newSession := s.ensureSession("thread-search-camel-new", map[string]any{"title": "New"})
	oldSession.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	newSession.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"updated_at","sortOrder":"asc","select":["thread_id"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-camel-old" {
		t.Fatalf("first thread=%v want thread-search-camel-old", threads[0]["thread_id"])
	}
}

func TestThreadSearchAcceptsPageSizeAlias(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-pagesize-1", map[string]any{"title": "One"})
	second := s.ensureSession("thread-search-pagesize-2", map[string]any{"title": "Two"})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"pageSize":1,"sortBy":"updated_at","sortOrder":"desc","select":["thread_id"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-pagesize-2" {
		t.Fatalf("first thread=%v want thread-search-pagesize-2", threads[0]["thread_id"])
	}
}

func TestThreadSearchAcceptsCamelCaseSortValuesAndSelectFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	oldSession := s.ensureSession("thread-search-camel-value-old", map[string]any{"title": "Old"})
	newSession := s.ensureSession("thread-search-camel-value-new", map[string]any{"title": "New"})
	oldSession.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	newSession.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"updatedAt","sortOrder":"asc","select":["threadId","updatedAt","values"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-camel-value-old" {
		t.Fatalf("first thread=%v want thread-search-camel-value-old", threads[0]["thread_id"])
	}
	if _, ok := threads[0]["updated_at"]; !ok {
		t.Fatalf("missing updated_at in %#v", threads[0])
	}
	if _, ok := threads[0]["values"]; !ok {
		t.Fatalf("missing values in %#v", threads[0])
	}
	if _, ok := threads[0]["updatedAt"]; ok {
		t.Fatalf("unexpected updatedAt alias in %#v", threads[0])
	}
}

func TestThreadSearchAcceptsCamelCaseRunIDSort(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-run-b", map[string]any{"run_id": "run-b"})
	second := s.ensureSession("thread-search-run-a", map[string]any{"run_id": "run-a"})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"runId","sortOrder":"asc","select":["threadId","runId"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-run-a" {
		t.Fatalf("first thread=%v want thread-search-run-a", threads[0]["thread_id"])
	}
	if threads[0]["run_id"] != "run-a" {
		t.Fatalf("run_id=%v want run-a", threads[0]["run_id"])
	}
	if _, ok := threads[0]["runId"]; ok {
		t.Fatalf("unexpected runId alias in %#v", threads[0])
	}
}

func TestThreadSearchAcceptsCamelCaseCheckpointIDSort(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-checkpoint-b", map[string]any{"checkpoint_id": "cp-b"})
	second := s.ensureSession("thread-search-checkpoint-a", map[string]any{"checkpoint_id": "cp-a"})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"checkpointId","sortOrder":"asc","select":["threadId","checkpointId"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-checkpoint-a" {
		t.Fatalf("first thread=%v want thread-search-checkpoint-a", threads[0]["thread_id"])
	}
	if threads[0]["checkpoint_id"] != "cp-a" {
		t.Fatalf("checkpoint_id=%v want cp-a", threads[0]["checkpoint_id"])
	}
	if _, ok := threads[0]["checkpointId"]; ok {
		t.Fatalf("unexpected checkpointId alias in %#v", threads[0])
	}
}

func TestThreadSearchAcceptsCamelCaseParentCheckpointIDSort(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-parent-checkpoint-b", map[string]any{"parent_checkpoint_id": "cp-parent-b"})
	second := s.ensureSession("thread-search-parent-checkpoint-a", map[string]any{"parent_checkpoint_id": "cp-parent-a"})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"parentCheckpointId","sortOrder":"asc","select":["threadId","parentCheckpointId"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-parent-checkpoint-a" {
		t.Fatalf("first thread=%v want thread-search-parent-checkpoint-a", threads[0]["thread_id"])
	}
	if threads[0]["parent_checkpoint_id"] != "cp-parent-a" {
		t.Fatalf("parent_checkpoint_id=%v want cp-parent-a", threads[0]["parent_checkpoint_id"])
	}
	if _, ok := threads[0]["parentCheckpointId"]; ok {
		t.Fatalf("unexpected parentCheckpointId alias in %#v", threads[0])
	}
}

func TestThreadSearchAcceptsCamelCaseRuntimeSortFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-runtime-b", map[string]any{
		"model_name":       "qwen/Qwen3.5-9B",
		"reasoning_effort": "medium",
		"thinking_enabled": true,
		"is_plan_mode":     true,
		"subagent_enabled": true,
		"temperature":      0.8,
		"max_tokens":       512,
	})
	second := s.ensureSession("thread-search-runtime-a", map[string]any{
		"model_name":       "deepseek/deepseek-r1",
		"reasoning_effort": "high",
		"thinking_enabled": false,
		"is_plan_mode":     false,
		"subagent_enabled": false,
		"temperature":      0.2,
		"max_tokens":       128,
	})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	tests := []struct {
		name   string
		sortBy string
	}{
		{name: "model", sortBy: "modelName"},
		{name: "reasoning", sortBy: "reasoningEffort"},
		{name: "thinking", sortBy: "thinkingEnabled"},
		{name: "plan", sortBy: "isPlanMode"},
		{name: "subagent", sortBy: "subagentEnabled"},
		{name: "temperature", sortBy: "temperature"},
		{name: "max_tokens", sortBy: "maxTokens"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(
				ts.URL+"/threads/search",
				"application/json",
				strings.NewReader(`{"limit":10,"sortBy":"`+tc.sortBy+`","sortOrder":"asc","select":["threadId","modelName","reasoningEffort","thinkingEnabled","isPlanMode","subagentEnabled","temperature","maxTokens"]}`),
			)
			if err != nil {
				t.Fatalf("search request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
			}

			var threads []map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(threads) < 2 {
				t.Fatalf("threads=%#v", threads)
			}
			if threads[0]["thread_id"] != "thread-search-runtime-a" {
				t.Fatalf("first thread=%v want thread-search-runtime-a", threads[0]["thread_id"])
			}
		})
	}
}

func TestThreadSearchAcceptsCamelCaseCheckpointSelectFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-checkpoint-select", map[string]any{
		"checkpoint_id":               "cp-1",
		"parent_checkpoint_id":        "cp-parent-1",
		"checkpoint_ns":               "ns-1",
		"parent_checkpoint_ns":        "ns-parent-1",
		"checkpoint_thread_id":        "checkpoint-thread-1",
		"parent_checkpoint_thread_id": "checkpoint-thread-parent-1",
	})

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["threadId","checkpointId","parentCheckpointId","checkpointNs","parentCheckpointNs","checkpointThreadId","parentCheckpointThreadId","checkpoint","parentCheckpoint"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-checkpoint-select" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	if selected["checkpoint_id"] != "cp-1" || selected["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["checkpoint_ns"] != "ns-1" || selected["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["checkpoint_thread_id"] != "checkpoint-thread-1" || selected["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("selected=%#v", selected)
	}
	checkpoint, _ := selected["checkpoint"].(map[string]any)
	if checkpoint["checkpoint_id"] != "cp-1" || checkpoint["checkpoint_ns"] != "ns-1" || checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", selected["checkpoint"])
	}
	parentCheckpoint, _ := selected["parent_checkpoint"].(map[string]any)
	if parentCheckpoint["checkpoint_id"] != "cp-parent-1" || parentCheckpoint["checkpoint_ns"] != "ns-parent-1" || parentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", selected["parent_checkpoint"])
	}
	for _, alias := range []string{"checkpointId", "parentCheckpointId", "checkpointNs", "parentCheckpointNs", "checkpointThreadId", "parentCheckpointThreadId", "parentCheckpoint"} {
		if _, ok := selected[alias]; ok {
			t.Fatalf("unexpected %s alias in %#v", alias, selected)
		}
	}
}

func TestThreadSearchAcceptsCamelCaseCheckpointSummarySortFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-checkpoint-summary-b", map[string]any{
		"checkpoint_ns":               "ns-b",
		"parent_checkpoint_ns":        "parent-ns-b",
		"checkpoint_thread_id":        "checkpoint-thread-b",
		"parent_checkpoint_thread_id": "parent-checkpoint-thread-b",
	})
	second := s.ensureSession("thread-search-checkpoint-summary-a", map[string]any{
		"checkpoint_ns":               "ns-a",
		"parent_checkpoint_ns":        "parent-ns-a",
		"checkpoint_thread_id":        "checkpoint-thread-a",
		"parent_checkpoint_thread_id": "parent-checkpoint-thread-a",
	})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	tests := []struct {
		name   string
		sortBy string
	}{
		{name: "checkpoint_ns", sortBy: "checkpointNs"},
		{name: "parent_checkpoint_ns", sortBy: "parentCheckpointNs"},
		{name: "checkpoint_thread_id", sortBy: "checkpointThreadId"},
		{name: "parent_checkpoint_thread_id", sortBy: "parentCheckpointThreadId"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(
				ts.URL+"/threads/search",
				"application/json",
				strings.NewReader(`{"limit":10,"sortBy":"`+tc.sortBy+`","sortOrder":"asc","select":["threadId"]}`),
			)
			if err != nil {
				t.Fatalf("search request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
			}

			var threads []map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(threads) < 2 {
				t.Fatalf("threads=%#v", threads)
			}
			if threads[0]["thread_id"] != "thread-search-checkpoint-summary-a" {
				t.Fatalf("first thread=%v want thread-search-checkpoint-summary-a", threads[0]["thread_id"])
			}
		})
	}
}

func TestThreadSearchIncludesCheckpointFieldsByDefault(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-checkpoint-default", map[string]any{
		"assistant_id":                "assistant-1",
		"graph_id":                    "graph-1",
		"run_id":                      "run-1",
		"step":                        3,
		"next":                        []any{"lead_agent"},
		"tasks":                       []any{map[string]any{"id": "task-1", "name": "lead_agent"}},
		"interrupts":                  []any{map[string]any{"value": "Need input"}},
		"checkpoint_id":               "cp-1",
		"parent_checkpoint_id":        "cp-parent-1",
		"checkpoint_ns":               "ns-1",
		"parent_checkpoint_ns":        "ns-parent-1",
		"checkpoint_thread_id":        "checkpoint-thread-1",
		"parent_checkpoint_thread_id": "checkpoint-thread-parent-1",
	})

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-checkpoint-default" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	if selected["checkpoint_id"] != "cp-1" || selected["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["checkpoint_ns"] != "ns-1" || selected["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["checkpoint_thread_id"] != "checkpoint-thread-1" || selected["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["step"] != float64(3) && selected["step"] != 3 {
		t.Fatalf("selected=%#v", selected)
	}
	next, _ := selected["next"].([]any)
	if len(next) != 1 || next[0] != "lead_agent" {
		t.Fatalf("selected=%#v", selected)
	}
	tasks, _ := selected["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
	interrupts, _ := selected["interrupts"].([]any)
	if len(interrupts) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["assistant_id"] != "assistant-1" || selected["graph_id"] != "graph-1" || selected["run_id"] != "run-1" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["agent_name"] != "" || selected["agent_type"] != "" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["title"] != "" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["mode"] != "flash" || selected["model_name"] != "" || selected["reasoning_effort"] != "minimal" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["thinking_enabled"] != true || selected["is_plan_mode"] != false || selected["subagent_enabled"] != false {
		t.Fatalf("selected=%#v", selected)
	}
	if value, ok := selected["temperature"]; !ok || value != nil {
		t.Fatalf("temperature=%#v in %#v", selected["temperature"], selected)
	}
	if value, ok := selected["max_tokens"]; !ok || value != nil {
		t.Fatalf("max_tokens=%#v in %#v", selected["max_tokens"], selected)
	}
	metadata, _ := selected["metadata"].(map[string]any)
	if metadata["assistant_id"] != "assistant-1" || metadata["graph_id"] != "graph-1" || metadata["run_id"] != "run-1" {
		t.Fatalf("metadata=%#v", selected["metadata"])
	}
	checkpoint, _ := selected["checkpoint"].(map[string]any)
	if checkpoint["checkpoint_id"] != "cp-1" || checkpoint["checkpoint_ns"] != "ns-1" || checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", selected["checkpoint"])
	}
	parentCheckpoint, _ := selected["parent_checkpoint"].(map[string]any)
	if parentCheckpoint["checkpoint_id"] != "cp-parent-1" || parentCheckpoint["checkpoint_ns"] != "ns-parent-1" || parentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", selected["parent_checkpoint"])
	}
}

func TestThreadSearchSelectProjectsExecutionSummaries(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-execution-select", map[string]any{
		"next":       []any{"lead_agent"},
		"tasks":      []any{map[string]any{"id": "task-1", "name": "lead_agent"}},
		"interrupts": []any{map[string]any{"value": "Need input"}},
	})

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["threadId","next","tasks","interrupts"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-execution-select" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	next, _ := selected["next"].([]any)
	if len(next) != 1 || next[0] != "lead_agent" {
		t.Fatalf("selected=%#v", selected)
	}
	tasks, _ := selected["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
	interrupts, _ := selected["interrupts"].([]any)
	if len(interrupts) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
}

func TestThreadSearchDefaultIncludesRecoveredValueSummaries(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-value-summaries", map[string]any{
		"title":   "Summary Thread",
		"todos":   []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox": map[string]any{"sandbox_id": "sb-1"},
		"viewed_images": map[string]any{
			"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"},
		},
		"artifacts": []any{"/tmp/report.html"},
		"thread_data": map[string]any{
			"workspace_path": "/tmp/workspace",
			"uploads_path":   "/tmp/uploads",
			"outputs_path":   "/tmp/outputs",
		},
		"uploaded_files": []any{
			map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt", "status": "uploaded"},
		},
	})

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-value-summaries" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	if selected["title"] != "Summary Thread" {
		t.Fatalf("selected=%#v", selected)
	}
	artifactsSummary, _ := selected["artifacts"].([]any)
	if len(artifactsSummary) != 1 || artifactsSummary[0] != "/tmp/report.html" {
		t.Fatalf("selected=%#v", selected)
	}
	todosSummary, _ := selected["todos"].([]any)
	if len(todosSummary) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
	sandboxSummary, _ := selected["sandbox"].(map[string]any)
	if sandboxSummary["sandbox_id"] != "sb-1" {
		t.Fatalf("selected=%#v", selected)
	}
	threadDataSummary, _ := selected["thread_data"].(map[string]any)
	if threadDataSummary["workspace_path"] != "/tmp/workspace" || threadDataSummary["uploads_path"] != "/tmp/uploads" || threadDataSummary["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("selected=%#v", selected)
	}
	uploadedFilesSummary, _ := selected["uploaded_files"].([]any)
	if len(uploadedFilesSummary) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
	viewedImagesSummary, _ := selected["viewed_images"].(map[string]any)
	if len(viewedImagesSummary) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
	values, _ := selected["values"].(map[string]any)
	if values["title"] != "Summary Thread" {
		t.Fatalf("values=%#v", values)
	}
	artifacts, _ := values["artifacts"].([]any)
	if len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("values=%#v", values)
	}
	threadData, _ := values["thread_data"].(map[string]any)
	if threadData["workspace_path"] != "/tmp/workspace" || threadData["uploads_path"] != "/tmp/uploads" || threadData["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("values=%#v", values)
	}
	uploadedFiles, _ := values["uploaded_files"].([]any)
	if len(uploadedFiles) != 1 {
		t.Fatalf("values=%#v", values)
	}
}

func TestThreadSearchAcceptsValueSummarySelectFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-value-select", map[string]any{
		"title":   "Selected Summary",
		"todos":   []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox": map[string]any{"sandbox_id": "sb-1"},
		"viewed_images": map[string]any{
			"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"},
		},
		"artifacts": []any{"/tmp/report.html"},
		"thread_data": map[string]any{
			"workspace_path": "/tmp/workspace",
			"uploads_path":   "/tmp/uploads",
			"outputs_path":   "/tmp/outputs",
		},
		"uploaded_files": []any{
			map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt", "status": "uploaded"},
		},
	})

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["threadId","title","todos","sandbox","artifacts","viewedImages","threadData","uploadedFiles"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-value-select" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	if selected["title"] != "Selected Summary" {
		t.Fatalf("selected=%#v", selected)
	}
	if _, ok := selected["viewedImages"]; ok {
		t.Fatalf("selected=%#v", selected)
	}
	if _, ok := selected["threadData"]; ok {
		t.Fatalf("selected=%#v", selected)
	}
	if _, ok := selected["uploadedFiles"]; ok {
		t.Fatalf("selected=%#v", selected)
	}
	todos, _ := selected["todos"].([]any)
	if len(todos) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
	sandbox, _ := selected["sandbox"].(map[string]any)
	if sandbox["sandbox_id"] != "sb-1" {
		t.Fatalf("selected=%#v", selected)
	}
	artifacts, _ := selected["artifacts"].([]any)
	if len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("selected=%#v", selected)
	}
	viewedImages, _ := selected["viewed_images"].(map[string]any)
	if len(viewedImages) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
	threadData, _ := selected["thread_data"].(map[string]any)
	if threadData["workspace_path"] != "/tmp/workspace" || threadData["uploads_path"] != "/tmp/uploads" || threadData["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("selected=%#v", selected)
	}
	uploadedFiles, _ := selected["uploaded_files"].([]any)
	if len(uploadedFiles) != 1 {
		t.Fatalf("selected=%#v", selected)
	}
}

func TestThreadSearchAcceptsSnakeCasePageSize(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-page-size-a", nil)
	s.ensureSession("thread-search-page-size-b", nil)

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"page_size":1,"sortBy":"threadId","sortOrder":"asc","select":["threadId"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("len=%d threads=%#v", len(threads), threads)
	}
}

func TestThreadSearchAcceptsCamelCaseCoreIDSelectFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-core-id-select", map[string]any{
		"assistant_id": "assistant-1",
		"graph_id":     "graph-1",
		"run_id":       "run-1",
	})

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["threadId","assistantId","graphId","runId"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-core-id-select" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	if selected["assistant_id"] != "assistant-1" || selected["graph_id"] != "graph-1" || selected["run_id"] != "run-1" {
		t.Fatalf("selected=%#v", selected)
	}
	if _, ok := selected["assistantId"]; ok {
		t.Fatalf("unexpected assistantId alias in %#v", selected)
	}
	if _, ok := selected["graphId"]; ok {
		t.Fatalf("unexpected graphId alias in %#v", selected)
	}
	if _, ok := selected["runId"]; ok {
		t.Fatalf("unexpected runId alias in %#v", selected)
	}
}

func TestThreadSearchAcceptsCamelCaseRuntimeSelectFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("thread-search-runtime-select", map[string]any{
		"agent_name":       "writer",
		"agent_type":       "deep_research",
		"model_name":       "deepseek/deepseek-r1",
		"reasoning_effort": "high",
		"thinking_enabled": false,
		"is_plan_mode":     true,
		"subagent_enabled": true,
		"temperature":      0.2,
		"max_tokens":       321,
	})

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["threadId","agentName","agentType","modelName","reasoningEffort","thinkingEnabled","isPlanMode","subagentEnabled","temperature","maxTokens"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) == 0 {
		t.Fatal("expected threads")
	}

	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == "thread-search-runtime-select" {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	if selected["model_name"] != "deepseek/deepseek-r1" || selected["reasoning_effort"] != "high" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["agent_name"] != "writer" || selected["agent_type"] != "deep_research" {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["thinking_enabled"] != false || selected["is_plan_mode"] != true || selected["subagent_enabled"] != true {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["temperature"] != 0.2 {
		t.Fatalf("selected=%#v", selected)
	}
	if selected["max_tokens"] != float64(321) && selected["max_tokens"] != int64(321) && selected["max_tokens"] != 321 {
		t.Fatalf("selected=%#v", selected)
	}
	for _, alias := range []string{"agentName", "agentType", "modelName", "reasoningEffort", "thinkingEnabled", "isPlanMode", "subagentEnabled", "maxTokens"} {
		if _, ok := selected[alias]; ok {
			t.Fatalf("unexpected %s alias in %#v", alias, selected)
		}
	}
}

func TestThreadSearchAcceptsCamelCaseAgentSortFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-agent-b", map[string]any{"agent_name": "writer", "agent_type": "research"})
	second := s.ensureSession("thread-search-agent-a", map[string]any{"agent_name": "planner", "agent_type": "analysis"})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	tests := []struct {
		name   string
		sortBy string
	}{
		{name: "agent_name", sortBy: "agentName"},
		{name: "agent_type", sortBy: "agentType"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(
				ts.URL+"/threads/search",
				"application/json",
				strings.NewReader(`{"limit":10,"sortBy":"`+tc.sortBy+`","sortOrder":"asc","select":["threadId","agentName","agentType"]}`),
			)
			if err != nil {
				t.Fatalf("search request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
			}

			var threads []map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(threads) < 2 {
				t.Fatalf("threads=%#v", threads)
			}
			if threads[0]["thread_id"] != "thread-search-agent-a" {
				t.Fatalf("first thread=%v want thread-search-agent-a", threads[0]["thread_id"])
			}
		})
	}
}

func TestThreadSearchSupportsStatusSort(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-status-busy", nil)
	second := s.ensureSession("thread-search-status-idle", nil)
	first.Status = "busy"
	second.Status = "idle"
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"status","sortOrder":"asc","select":["threadId","status"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-status-busy" {
		t.Fatalf("first thread=%v want thread-search-status-busy", threads[0]["thread_id"])
	}
	if threads[0]["status"] != "busy" {
		t.Fatalf("status=%v want busy", threads[0]["status"])
	}
}

func TestThreadSearchSupportsTitleSort(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-title-z", map[string]any{"title": "Zeta"})
	second := s.ensureSession("thread-search-title-a", map[string]any{"title": "Alpha"})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"title","sortOrder":"asc","select":["threadId","title"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-title-a" {
		t.Fatalf("first thread=%v want thread-search-title-a", threads[0]["thread_id"])
	}
	if threads[0]["title"] != "Alpha" {
		t.Fatalf("title=%#v want Alpha", threads[0]["title"])
	}
}

func TestThreadSearchSortsByCamelCaseAssistantID(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-assistant-b", map[string]any{"assistant_id": "beta"})
	second := s.ensureSession("thread-search-assistant-a", map[string]any{"assistant_id": "alpha"})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"assistantId","sortOrder":"asc","select":["threadId"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-assistant-a" {
		t.Fatalf("first thread=%v want thread-search-assistant-a", threads[0]["thread_id"])
	}
}

func TestThreadSearchSortsByCamelCaseGraphID(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-graph-b", map[string]any{"graph_id": "beta"})
	second := s.ensureSession("thread-search-graph-a", map[string]any{"graph_id": "alpha"})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"graphId","sortOrder":"asc","select":["threadId"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-graph-a" {
		t.Fatalf("first thread=%v want thread-search-graph-a", threads[0]["thread_id"])
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

func TestGatewayModelsFromEnvJSONAcceptsCamelCaseFields(t *testing.T) {
	models := gatewayModelsFromEnv(`[
		{"name":"flash","model":"qwen/Qwen3.5-9B","displayName":"Flash","supportsThinking":false},
		{"name":"pro","model":"deepseek/deepseek-r1","displayName":"Pro","supportsThinking":true,"supportsReasoningEffort":true}
	]`)
	if len(models) != 2 {
		t.Fatalf("len=%d want 2", len(models))
	}
	if models[0].DisplayName != "Flash" {
		t.Fatalf("display_name=%q want Flash", models[0].DisplayName)
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

func TestMCPConfigPutAcceptsCamelCaseFields(t *testing.T) {
	_, ts := newCompatTestServer(t)

	body := `{
		"mcpServers": {
			"github": {
				"enabled": true,
				"type": "http",
				"url": "https://example.com/mcp",
				"headers": {"Authorization":"Bearer token"},
				"oauth": {
					"enabled": true,
					"tokenUrl": "https://example.com/token",
					"grantType": "client_credentials",
					"clientId": "client-id",
					"clientSecret": "client-secret",
					"refreshSkewSeconds": 42,
					"extraTokenParams": {"aud":"demo"}
				}
			}
		}
	}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/mcp/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mcp put request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var cfg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	servers, ok := cfg["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers=%#v", cfg["mcp_servers"])
	}
	githubServer, ok := servers["github"].(map[string]any)
	if !ok {
		t.Fatalf("github server=%#v", servers["github"])
	}
	oauth, ok := githubServer["oauth"].(map[string]any)
	if !ok {
		t.Fatalf("oauth=%#v", githubServer["oauth"])
	}
	if oauth["token_url"] != "https://example.com/token" {
		t.Fatalf("token_url=%v", oauth["token_url"])
	}
	if oauth["client_id"] != "client-id" || oauth["client_secret"] != "client-secret" {
		t.Fatalf("oauth=%#v", oauth)
	}
	if oauth["refresh_skew_seconds"] != float64(42) {
		t.Fatalf("refresh_skew_seconds=%v", oauth["refresh_skew_seconds"])
	}
}

func TestMCPConfigPutHonorsExplicitZeroRefreshSkew(t *testing.T) {
	_, ts := newCompatTestServer(t)

	body := `{
		"mcpServers": {
			"github": {
				"enabled": true,
				"oauth": {
					"enabled": true,
					"tokenUrl": "https://example.com/token",
					"refreshSkewSeconds": 0
				}
			}
		}
	}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/mcp/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mcp put request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var cfg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	servers := cfg["mcp_servers"].(map[string]any)
	server := servers["github"].(map[string]any)
	oauth := server["oauth"].(map[string]any)
	if oauth["refresh_skew_seconds"] != float64(0) {
		t.Fatalf("refresh_skew_seconds=%v want 0", oauth["refresh_skew_seconds"])
	}
}

func TestGatewayMCPConfigFromEnvHonorsExplicitZeroRefreshSkew(t *testing.T) {
	t.Setenv("DEERFLOW_MCP_CONFIG", `{"mcp_servers":{"github":{"enabled":true,"oauth":{"enabled":true,"token_url":"https://example.com/token","refresh_skew_seconds":0}}}}`)
	cfg := gatewayMCPConfigFromEnv(os.Getenv("DEERFLOW_MCP_CONFIG"))
	server, ok := cfg.MCPServers["github"]
	if !ok {
		t.Fatalf("missing github server: %#v", cfg)
	}
	if server.OAuth == nil {
		t.Fatalf("missing oauth: %#v", server)
	}
	if server.OAuth.RefreshSkewSecond != 0 {
		t.Fatalf("refresh_skew_seconds=%d want 0", server.OAuth.RefreshSkewSecond)
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

func TestLoadAgentsFromFilesAcceptsCamelCaseToolGroups(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	dir := s.agentDir("writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{"name":"writer","description":"Writer","model":"qwen/Qwen3.5-9B","toolGroups":["web"]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("Write clearly."), 0o644); err != nil {
		t.Fatalf("write soul: %v", err)
	}

	loaded := s.loadAgentsFromFiles()
	got, ok := loaded["writer"]
	if !ok {
		t.Fatalf("missing agent: %#v", loaded)
	}
	if len(got.ToolGroups) != 1 || got.ToolGroups[0] != "web" {
		t.Fatalf("tool_groups=%#v", got.ToolGroups)
	}
	if got.Soul != "Write clearly." {
		t.Fatalf("soul=%q", got.Soul)
	}
}

func TestLoadAgentsFromFilesAcceptsSoulOnlyDirectories(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	dir := filepath.Join(s.agentsRoot(), "writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("Write clearly."), 0o644); err != nil {
		t.Fatalf("write soul: %v", err)
	}

	loaded := s.loadAgentsFromFiles()
	got, ok := loaded["writer"]
	if !ok {
		t.Fatalf("missing agent: %#v", loaded)
	}
	if got.Name != "writer" || got.Soul != "Write clearly." {
		t.Fatalf("agent=%#v", got)
	}
}

func TestLoadAgentsFromFilesAcceptsModelNameAliases(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	dir := filepath.Join(s.agentsRoot(), "writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{"name":"writer","modelName":"qwen/Qwen3.5-9B"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded := s.loadAgentsFromFiles()
	got, ok := loaded["writer"]
	if !ok || got.Model == nil || *got.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("agent=%#v", got)
	}
}

func TestLoadAgentsFromFilesAcceptsWrappedAgentObject(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	dir := filepath.Join(s.agentsRoot(), "writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"agent":{
			"name":"writer",
			"description":"Writer",
			"modelName":"qwen/Qwen3.5-9B",
			"toolGroups":["web"]
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded := s.loadAgentsFromFiles()
	got, ok := loaded["writer"]
	if !ok {
		t.Fatalf("missing agent: %#v", loaded)
	}
	if got.Description != "Writer" || got.Model == nil || *got.Model != "qwen/Qwen3.5-9B" || len(got.ToolGroups) != 1 || got.ToolGroups[0] != "web" {
		t.Fatalf("agent=%#v", got)
	}
}

func TestLoadAgentsFromFilesAcceptsWrappedConfigObject(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	dir := filepath.Join(s.agentsRoot(), "writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"config":{
			"name":"writer",
			"description":"Writer",
			"modelName":"qwen/Qwen3.5-9B",
			"toolGroups":["web"]
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded := s.loadAgentsFromFiles()
	got, ok := loaded["writer"]
	if !ok {
		t.Fatalf("missing agent: %#v", loaded)
	}
	if got.Description != "Writer" || got.Model == nil || *got.Model != "qwen/Qwen3.5-9B" || len(got.ToolGroups) != 1 || got.ToolGroups[0] != "web" {
		t.Fatalf("agent=%#v", got)
	}
}

func TestLoadAgentsFromFilesAcceptsDataWrapper(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	dir := filepath.Join(s.agentsRoot(), "writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"data":{
			"name":"writer",
			"description":"Writer",
			"modelName":"qwen/Qwen3.5-9B",
			"toolGroups":["web"]
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded := s.loadAgentsFromFiles()
	got, ok := loaded["writer"]
	if !ok {
		t.Fatalf("missing agent: %#v", loaded)
	}
	if got.Description != "Writer" || got.Model == nil || *got.Model != "qwen/Qwen3.5-9B" || len(got.ToolGroups) != 1 || got.ToolGroups[0] != "web" {
		t.Fatalf("agent=%#v", got)
	}
}

func TestLoadUserProfileFromFileAcceptsJSONContent(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{"content":"Legacy profile from JSON"}`
	if err := os.WriteFile(s.userProfilePath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write user profile: %v", err)
	}

	content, ok := s.loadUserProfileFromFile()
	if !ok {
		t.Fatal("expected user profile to load")
	}
	if content != "Legacy profile from JSON" {
		t.Fatalf("content=%q", content)
	}
}

func TestLoadUserProfileFromFileAcceptsJSONNullContent(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{"content":null}`
	if err := os.WriteFile(s.userProfilePath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write user profile: %v", err)
	}

	content, ok := s.loadUserProfileFromFile()
	if !ok {
		t.Fatal("expected user profile to load")
	}
	if content != "" {
		t.Fatalf("content=%q", content)
	}
}

func TestLoadUserProfileFromFileAcceptsJSONString(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `"Legacy profile from string JSON"`
	if err := os.WriteFile(s.userProfilePath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write user profile: %v", err)
	}

	content, ok := s.loadUserProfileFromFile()
	if !ok {
		t.Fatal("expected user profile to load")
	}
	if content != "Legacy profile from string JSON" {
		t.Fatalf("content=%q", content)
	}
}

func TestLoadUserProfileFromFileAcceptsDataWrapper(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{"data":{"content":"Legacy profile from wrapped JSON"}}`
	if err := os.WriteFile(s.userProfilePath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write user profile: %v", err)
	}

	content, ok := s.loadUserProfileFromFile()
	if !ok {
		t.Fatal("expected user profile to load")
	}
	if content != "Legacy profile from wrapped JSON" {
		t.Fatalf("content=%q", content)
	}
}

func TestLoadUserProfileFromFileAcceptsWrappedJSONString(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{"data":"Legacy profile from wrapped string JSON"}`
	if err := os.WriteFile(s.userProfilePath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write user profile: %v", err)
	}

	content, ok := s.loadUserProfileFromFile()
	if !ok {
		t.Fatal("expected user profile to load")
	}
	if content != "Legacy profile from wrapped string JSON" {
		t.Fatalf("content=%q", content)
	}
}

func TestLoadMemoryFromFileAcceptsSnakeCaseFields(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{
		"version":"1.0",
		"last_updated":"2026-01-01T00:00:00Z",
		"user":{
			"work_context":{"summary":"Work","updated_at":"2026-01-01T00:00:00Z"},
			"personal_context":{"summary":"Personal","updated_at":"2026-01-01T00:00:00Z"},
			"top_of_mind":{"summary":"Mind","updated_at":"2026-01-01T00:00:00Z"}
		},
		"history":{
			"recent_months":{"summary":"Recent","updated_at":"2026-01-01T00:00:00Z"},
			"earlier_context":{"summary":"Earlier","updated_at":"2026-01-01T00:00:00Z"},
			"long_term_background":{"summary":"Long","updated_at":"2026-01-01T00:00:00Z"}
		},
		"facts":[
			{"id":"f1","content":"Fact","category":"general","confidence":0.8,"created_at":"2026-01-01T00:00:00Z","source":"thread-1"}
		]
	}`
	if err := os.WriteFile(s.memoryPath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	mem, ok := s.loadMemoryFromFile()
	if !ok {
		t.Fatal("expected memory file to load")
	}
	if mem.LastUpdated != "2026-01-01T00:00:00Z" {
		t.Fatalf("lastUpdated=%q", mem.LastUpdated)
	}
	if mem.User.WorkContext.Summary != "Work" || mem.History.LongTermBackground.Summary != "Long" {
		t.Fatalf("unexpected memory=%#v", mem)
	}
	if len(mem.Facts) != 1 || mem.Facts[0].CreatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("facts=%#v", mem.Facts)
	}
}

func TestLoadMemoryFromFileAcceptsFlatLegacySections(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{
		"version":"1.0",
		"lastUpdated":"2026-01-01T00:00:00Z",
		"workContext":{"summary":"Work","updatedAt":"2026-01-01T00:00:00Z"},
		"personal_context":{"summary":"Personal","updated_at":"2026-01-01T00:00:00Z"},
		"topOfMind":{"summary":"Mind","updatedAt":"2026-01-01T00:00:00Z"},
		"recent_months":{"summary":"Recent","updated_at":"2026-01-01T00:00:00Z"},
		"earlierContext":{"summary":"Earlier","updatedAt":"2026-01-01T00:00:00Z"},
		"long_term_background":{"summary":"Long","updated_at":"2026-01-01T00:00:00Z"},
		"facts":[]
	}`
	if err := os.WriteFile(s.memoryPath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	mem, ok := s.loadMemoryFromFile()
	if !ok {
		t.Fatal("expected memory file to load")
	}
	if mem.User.WorkContext.Summary != "Work" || mem.User.PersonalContext.Summary != "Personal" || mem.User.TopOfMind.Summary != "Mind" {
		t.Fatalf("unexpected user memory=%#v", mem.User)
	}
	if mem.History.RecentMonths.Summary != "Recent" || mem.History.EarlierContext.Summary != "Earlier" || mem.History.LongTermBackground.Summary != "Long" {
		t.Fatalf("unexpected history memory=%#v", mem.History)
	}
}

func TestLoadMemoryFromFileAcceptsWrappedMemoryObject(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{
		"memory":{
			"version":"1.0",
			"lastUpdated":"2026-01-01T00:00:00Z",
			"user":{"workContext":{"summary":"Work","updatedAt":"2026-01-01T00:00:00Z"}},
			"history":{},
			"facts":[]
		}
	}`
	if err := os.WriteFile(s.memoryPath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	mem, ok := s.loadMemoryFromFile()
	if !ok {
		t.Fatal("expected memory file to load")
	}
	if mem.Version != "1.0" || mem.LastUpdated != "2026-01-01T00:00:00Z" {
		t.Fatalf("memory=%#v", mem)
	}
	if mem.User.WorkContext.Summary != "Work" {
		t.Fatalf("memory=%#v", mem)
	}
}

func TestLoadMemoryFromFileAcceptsDataWrapper(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{
		"data":{
			"version":"1.0",
			"lastUpdated":"2026-01-01T00:00:00Z",
			"user":{"workContext":{"summary":"Work","updatedAt":"2026-01-01T00:00:00Z"}},
			"history":{},
			"facts":[]
		}
	}`
	if err := os.WriteFile(s.memoryPath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	mem, ok := s.loadMemoryFromFile()
	if !ok || mem.User.WorkContext.Summary != "Work" {
		t.Fatalf("memory=%#v ok=%v", mem, ok)
	}
}

func TestLoadMemoryFromFileAcceptsWrappedFacts(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	raw := `{
		"version":"1.0",
		"lastUpdated":"2026-01-01T00:00:00Z",
		"user":{},
		"history":{},
		"facts":{"items":[{"id":"f1","content":"Fact","category":"general","confidence":0.8,"createdAt":"2026-01-01T00:00:00Z","source":"thread-1"}]}
	}`
	if err := os.WriteFile(s.memoryPath(), []byte(raw), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}

	mem, ok := s.loadMemoryFromFile()
	if !ok {
		t.Fatal("expected memory file to load")
	}
	if len(mem.Facts) != 1 || mem.Facts[0].ID != "f1" || mem.Facts[0].CreatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("facts=%#v", mem.Facts)
	}
}

func TestLoadGatewayStateAcceptsLegacyFieldNames(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"models":{
			"flash":{"name":"flash","model":"qwen/Qwen3.5-9B","displayName":"Flash","supportsThinking":true}
		},
		"mcp_config":{
			"mcpServers":{
				"github":{
					"enabled":true,
					"oauth":{"enabled":true,"tokenUrl":"https://example.com/token","refreshSkewSeconds":0}
				}
			}
		},
		"agents":{
			"writer":{"name":"writer","description":"Writer","toolGroups":["web"]}
		},
		"user_profile":"Legacy profile",
		"memory":{
			"version":"1.0",
			"last_updated":"2026-01-01T00:00:00Z",
			"user":{"work_context":{"summary":"Work","updated_at":"2026-01-01T00:00:00Z"}},
			"history":{},
			"facts":[]
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	model, ok := loaded.findModelLocked("flash")
	if !ok || model.DisplayName != "Flash" || !model.SupportsThinking {
		t.Fatalf("model=%#v ok=%v", model, ok)
	}
	if loaded.mcpConfig.MCPServers["github"].OAuth == nil || loaded.mcpConfig.MCPServers["github"].OAuth.RefreshSkewSecond != 0 {
		t.Fatalf("mcp=%#v", loaded.mcpConfig)
	}
	if loaded.getAgentsLocked()["writer"].ToolGroups == nil || len(loaded.getAgentsLocked()["writer"].ToolGroups) != 1 {
		t.Fatalf("agents=%#v", loaded.getAgentsLocked())
	}
	if loaded.getUserProfileLocked() != "Legacy profile" {
		t.Fatalf("userProfile=%q", loaded.getUserProfileLocked())
	}
	if loaded.getMemoryLocked().LastUpdated != "2026-01-01T00:00:00Z" {
		t.Fatalf("memory=%#v", loaded.getMemoryLocked())
	}
}

func TestLoadGatewayStateUsesModelMapKeyWhenNameMissing(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"models":{
			"flash":{"model":"qwen/Qwen3.5-9B","displayName":"Flash"}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	model, ok := loaded.findModelLocked("flash")
	if !ok {
		t.Fatalf("models=%#v", loaded.getModelsLocked())
	}
	if model.Name != "flash" || model.ID != "flash" || model.DisplayName != "Flash" {
		t.Fatalf("model=%#v", model)
	}
}

func TestLoadGatewayStateAcceptsModelNameAliases(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"models":{"flash":{"modelName":"qwen/Qwen3.5-9B","displayName":"Flash"}},
		"agents":{"writer":{"name":"writer","model_name":"qwen/Qwen3.5-9B"}}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	model, ok := loaded.findModelLocked("flash")
	if !ok || model.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("model=%#v", model)
	}
	agent, ok := loaded.getAgentsLocked()["writer"]
	if !ok || agent.Model == nil || *agent.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("agent=%#v", agent)
	}
}

func TestLoadGatewayStateAcceptsArrayCollections(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills", "public", "demo-skill")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: demo-skill
description: Demo skill
category: public
license: MIT
---
# Demo
`
	if err := os.WriteFile(filepath.Join(skillsRoot, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	raw := `{
		"models":[{"id":"flash","displayName":"Flash"}],
		"skills":[{"name":"demo-skill","description":"Demo","category":"public","license":"MIT","enabled":true}],
		"agents":[{"name":"writer","description":"Writer","toolGroups":["web"]}]
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	model, ok := loaded.findModelLocked("flash")
	if !ok || model.DisplayName != "Flash" {
		t.Fatalf("models=%#v", loaded.getModelsLocked())
	}
	skill, ok := loaded.getSkillsLocked()["demo-skill"]
	if !ok || !skill.Enabled {
		t.Fatalf("skills=%#v", loaded.getSkillsLocked())
	}
	agent, ok := loaded.getAgentsLocked()["writer"]
	if !ok || len(agent.ToolGroups) != 1 || agent.ToolGroups[0] != "web" {
		t.Fatalf("agents=%#v", loaded.getAgentsLocked())
	}
}

func TestLoadGatewayStateAcceptsWrappedStateObject(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"state":{
			"models":{"flash":{"modelName":"qwen/Qwen3.5-9B","displayName":"Flash"}},
			"agents":{"writer":{"name":"writer","model_name":"qwen/Qwen3.5-9B"}}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	model, ok := loaded.findModelLocked("flash")
	if !ok || model.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("model=%#v", model)
	}
	agent, ok := loaded.getAgentsLocked()["writer"]
	if !ok || agent.Model == nil || *agent.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("agent=%#v", agent)
	}
}

func TestLoadGatewayStateAcceptsWrappedGatewayObject(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"gateway":{
			"userProfile":"Wrapped profile",
			"memory":{"version":"1.0","lastUpdated":"2026-01-01T00:00:00Z","user":{},"history":{},"facts":[]}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	if loaded.getUserProfileLocked() != "Wrapped profile" {
		t.Fatalf("userProfile=%q", loaded.getUserProfileLocked())
	}
	if loaded.getMemoryLocked().LastUpdated != "2026-01-01T00:00:00Z" {
		t.Fatalf("memory=%#v", loaded.getMemoryLocked())
	}
}

func TestLoadGatewayStateAcceptsWrappedUIStateObject(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"ui_state":{
			"userProfile":"Wrapped UI profile",
			"models":{"flash":{"modelName":"qwen/Qwen3.5-9B","displayName":"Flash"}}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	if loaded.getUserProfileLocked() != "Wrapped UI profile" {
		t.Fatalf("userProfile=%q", loaded.getUserProfileLocked())
	}
	model, ok := loaded.findModelLocked("flash")
	if !ok || model.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("model=%#v", model)
	}
}

func TestLoadGatewayStateAcceptsDataWrapper(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"data":{
			"userProfile":"Wrapped data profile",
			"models":{"flash":{"modelName":"qwen/Qwen3.5-9B","displayName":"Flash"}}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{dataRoot: root, models: map[string]gatewayModel{}, agents: map[string]gatewayAgent{}}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}
	if loaded.getUserProfileLocked() != "Wrapped data profile" {
		t.Fatalf("userProfile=%q", loaded.getUserProfileLocked())
	}
	if model, ok := loaded.findModelLocked("flash"); !ok || model.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("model=%#v ok=%v", model, ok)
	}
}

func TestLoadGatewayStateAcceptsMCPAlias(t *testing.T) {
	root := t.TempDir()
	raw := `{
		"mcp":{
			"mcpServers":{
				"github":{
					"enabled":true,
					"oauth":{"enabled":true,"tokenUrl":"https://example.com/token","refreshSkewSeconds":0}
				}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	server := loaded.mcpConfig.MCPServers["github"]
	if !server.Enabled || server.OAuth == nil || server.OAuth.RefreshSkewSecond != 0 {
		t.Fatalf("mcp=%#v", loaded.mcpConfig)
	}
}

func TestGatewayModelsFromEnvUsesModelIDWhenNameMissing(t *testing.T) {
	t.Setenv("DEERFLOW_MODELS", `[{"id":"flash","displayName":"Flash"}]`)
	models := defaultGatewayModels("")
	model, ok := models["flash"]
	if !ok {
		t.Fatalf("models=%#v", models)
	}
	if model.Name != "flash" || model.ID != "flash" || model.Model != "flash" || model.DisplayName != "Flash" {
		t.Fatalf("model=%#v", model)
	}
}

func TestLoadGatewayStateAcceptsBooleanSkillStateMap(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills", "public", "demo-skill")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: demo-skill
description: Demo skill
category: public
license: MIT
---
# Demo
`
	if err := os.WriteFile(filepath.Join(skillsRoot, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	raw := `{
		"skills":{"demo-skill":true}
	}`
	if err := os.WriteFile(filepath.Join(root, "gateway_state.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write gateway_state.json: %v", err)
	}

	loaded := &Server{
		dataRoot: root,
		models:   map[string]gatewayModel{},
		agents:   map[string]gatewayAgent{},
	}
	if err := loaded.loadGatewayState(); err != nil {
		t.Fatalf("loadGatewayState: %v", err)
	}

	skill, ok := loaded.getSkillsLocked()["demo-skill"]
	if !ok {
		t.Fatalf("skills=%#v", loaded.getSkillsLocked())
	}
	if !skill.Enabled {
		t.Fatalf("skill not enabled: %#v", skill)
	}
}

func TestLoadPersistedThreadsAcceptsCamelCaseFields(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-camel", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"threadId":"thread-camel",
		"messages":[{"id":"m1","role":"human","content":"hello"}],
		"metadata":{"title":"Camel Thread"},
		"status":"idle",
		"createdAt":"2026-01-01T00:00:00Z",
		"updatedAt":"2026-01-02T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session, ok := s.sessions["thread-camel"]
	if !ok {
		t.Fatalf("missing session: %#v", s.sessions)
	}
	if session.Metadata["title"] != "Camel Thread" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not loaded: %#v", session)
	}
}

func TestLoadPersistedThreadsNormalizesCamelCaseMetadata(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-meta-camel", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"threadId":"thread-meta-camel",
		"messages":[],
		"metadata":{
			"viewedImages":{"/tmp/chart.png":{"base64":"xyz","mime_type":"image/png"}},
			"threadId":"thread-meta-camel",
			"assistantId":"assistant-1",
			"graphId":"graph-1",
			"runId":"run-1",
			"checkpointId":"cp-1",
			"parentCheckpointId":"cp-parent-1",
			"checkpointNs":"ns-1",
			"parentCheckpointNs":"ns-parent-1",
			"checkpointThreadId":"checkpoint-thread-1",
			"parentCheckpointThreadId":"checkpoint-thread-parent-1",
			"agentType":"deep_research",
			"modelName":"deepseek/deepseek-r1",
			"reasoningEffort":"high",
			"agentName":"writer",
			"thinkingEnabled":false,
			"isPlanMode":true,
			"subagentEnabled":true
		},
		"status":"idle",
		"createdAt":"2026-01-01T00:00:00Z",
		"updatedAt":"2026-01-02T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()
	session := s.sessions["thread-meta-camel"]
	if session == nil {
		t.Fatalf("missing session: %#v", s.sessions)
	}
	if _, ok := session.Metadata["viewed_images"]; !ok {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["model_name"] != "deepseek/deepseek-r1" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["agent_type"] != "deep_research" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["thread_id"] != "thread-meta-camel" || session.Metadata["assistant_id"] != "assistant-1" || session.Metadata["graph_id"] != "graph-1" || session.Metadata["run_id"] != "run-1" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["checkpoint_id"] != "cp-1" || session.Metadata["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["checkpoint_ns"] != "ns-1" || session.Metadata["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["checkpoint_thread_id"] != "checkpoint-thread-1" || session.Metadata["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["reasoning_effort"] != "high" || session.Metadata["agent_name"] != "writer" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["thinking_enabled"] != false || session.Metadata["is_plan_mode"] != true || session.Metadata["subagent_enabled"] != true {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
}

func TestLoadPersistedThreadsAcceptsWrappedThreadObject(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-wrapped", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"thread":{
			"threadId":"thread-wrapped",
			"messages":[{"id":"m1","role":"human","content":"hello"}],
			"metadata":{"title":"Wrapped Thread"},
			"status":"idle",
			"createdAt":"2026-01-01T00:00:00Z",
			"updatedAt":"2026-01-02T00:00:00Z"
		}
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-wrapped"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Metadata["title"] != "Wrapped Thread" || session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() {
		t.Fatalf("session=%#v", session)
	}
}

func TestLoadPersistedThreadsAcceptsDataWrapper(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-data-wrap", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"data":{
			"threadId":"thread-data-wrap",
			"messages":[{"id":"m1","role":"human","content":"hello"}],
			"metadata":{"title":"Data Wrapped Thread"},
			"status":"idle",
			"createdAt":"2026-01-01T00:00:00Z",
			"updatedAt":"2026-01-02T00:00:00Z"
		}
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}
	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()
	if session := s.sessions["thread-data-wrap"]; session == nil || session.Metadata["title"] != "Data Wrapped Thread" {
		t.Fatalf("sessions=%#v", s.sessions)
	}
}

func TestLoadPersistedThreadsAcceptsValuesStateObject(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-values-state", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{
			"messages":[
				{"id":"m1","type":"human","content":"hello from state"}
			],
			"artifacts":["/tmp/report.html"],
			"title":"Values Thread",
			"todos":[{"content":"task","status":"pending"}],
			"viewed_images":{"/tmp/chart.png":{"base64":"xyz","mime_type":"image/png"}}
		},
		"metadata":{
			"thread_id":"thread-values-state",
			"assistant_id":"assistant-1",
			"model_name":"qwen/Qwen3.5-9B",
			"step":7,
			"mode":"thinking"
		},
		"checkpoint":{"checkpoint_id":"cp-checkpoint-object","thread_id":"thread-values-state","checkpoint_ns":"ns-current"},
		"parent_checkpoint":{"checkpoint_id":"cp-parent-object","thread_id":"thread-values-state","checkpoint_ns":"ns-parent"},
		"checkpoint_id":"cp-top-level",
		"parent_checkpoint_id":"cp-parent-top-level",
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-values-state"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if len(session.Messages) != 1 || session.Messages[0].ID != "m1" || session.Messages[0].Content != "hello from state" {
		t.Fatalf("messages=%#v", session.Messages)
	}
	if session.Metadata["title"] != "Values Thread" || session.Metadata["assistant_id"] != "assistant-1" || session.Metadata["model_name"] != "qwen/Qwen3.5-9B" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["checkpoint_id"] != "cp-checkpoint-object" || session.Metadata["parent_checkpoint_id"] != "cp-parent-object" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["checkpoint_ns"] != "ns-current" || session.Metadata["parent_checkpoint_ns"] != "ns-parent" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["checkpoint_thread_id"] != "thread-values-state" || session.Metadata["parent_checkpoint_thread_id"] != "thread-values-state" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if artifacts := anyStringSlice(session.Metadata["artifacts"]); len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("artifacts=%#v metadata=%#v", artifacts, session.Metadata)
	}
	if _, ok := session.Metadata["viewed_images"]; !ok {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if artifacts := sessionArtifactPaths(session); len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("session artifacts=%#v", artifacts)
	}
	if session.CreatedAt.IsZero() {
		t.Fatalf("created_at not loaded: %#v", session)
	}
	if session.UpdatedAt.IsZero() || !session.UpdatedAt.Equal(session.CreatedAt) {
		t.Fatalf("updated_at=%v created_at=%v", session.UpdatedAt, session.CreatedAt)
	}
	if session.Status != "idle" {
		t.Fatalf("status=%q", session.Status)
	}
	state := s.getThreadState("thread-values-state")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if state.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("created_at=%q", state.CreatedAt)
	}
	if state.Metadata["step"] != float64(7) && state.Metadata["step"] != 7 {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	configurable, _ := state.Config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["reasoning_effort"] != "low" {
		t.Fatalf("configurable=%#v", configurable)
	}
}

func TestLoadPersistedThreadsAcceptsCamelCaseValueAliases(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-values-camel", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{
			"messages":[{"id":"m1","type":"human","content":"hello from state"}],
			"threadData":{
				"workspace_path":"/external/thread/workspace",
				"uploads_path":"/external/thread/uploads",
				"outputs_path":"/external/thread/outputs"
			},
			"uploadedFiles":[{"filename":"notes.txt","size":42,"path":"/external/thread/uploads/notes.txt","status":"uploaded"}]
		},
		"metadata":{"thread_id":"thread-values-camel"},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-values-camel"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	threadData, _ := session.Metadata["thread_data"].(map[string]any)
	if threadData["workspace_path"] != "/external/thread/workspace" || threadData["uploads_path"] != "/external/thread/uploads" || threadData["outputs_path"] != "/external/thread/outputs" {
		t.Fatalf("thread_data=%#v metadata=%#v", threadData, session.Metadata)
	}
	switch uploadedFiles := session.Metadata["uploaded_files"].(type) {
	case []map[string]any:
		if len(uploadedFiles) != 1 || uploadedFiles[0]["filename"] != "notes.txt" || uploadedFiles[0]["path"] != "/external/thread/uploads/notes.txt" {
			t.Fatalf("uploaded_files=%#v metadata=%#v", uploadedFiles, session.Metadata)
		}
	case []any:
		if len(uploadedFiles) != 1 {
			t.Fatalf("uploaded_files=%#v metadata=%#v", uploadedFiles, session.Metadata)
		}
		file, _ := uploadedFiles[0].(map[string]any)
		if file["filename"] != "notes.txt" || file["path"] != "/external/thread/uploads/notes.txt" {
			t.Fatalf("uploaded_files=%#v", uploadedFiles)
		}
	default:
		t.Fatalf("uploaded_files=%#v metadata=%#v", session.Metadata["uploaded_files"], session.Metadata)
	}

	state := s.getThreadState("thread-values-camel")
	if state == nil {
		t.Fatalf("state=nil")
	}
	restoredThreadData, _ := state.Values["thread_data"].(map[string]any)
	if restoredThreadData["workspace_path"] != "/external/thread/workspace" || restoredThreadData["uploads_path"] != "/external/thread/uploads" || restoredThreadData["outputs_path"] != "/external/thread/outputs" {
		t.Fatalf("thread_data=%#v", state.Values["thread_data"])
	}
	switch rawFiles := state.Values["uploaded_files"].(type) {
	case []map[string]any:
		if len(rawFiles) != 1 || rawFiles[0]["filename"] != "notes.txt" || rawFiles[0]["path"] != "/external/thread/uploads/notes.txt" {
			t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
		}
	case []any:
		if len(rawFiles) != 1 {
			t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
		}
		restoredFile, _ := rawFiles[0].(map[string]any)
		if restoredFile["filename"] != "notes.txt" || restoredFile["path"] != "/external/thread/uploads/notes.txt" {
			t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
		}
	default:
		t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
	}
}

func TestLoadPersistedThreadsAcceptsLegacyModelAlias(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-model-alias", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{"thread_id":"thread-model-alias","model":"doubao-seed-1.8"},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-model-alias"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Metadata["model_name"] != "doubao-seed-1.8" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	state := s.getThreadState("thread-model-alias")
	if state == nil {
		t.Fatalf("state=nil")
	}
	configurable, _ := state.Config["configurable"].(map[string]any)
	if configurable["model_name"] != "doubao-seed-1.8" {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["reasoning_effort"] != "minimal" {
		t.Fatalf("configurable=%#v", configurable)
	}
}

func TestLoadPersistedThreadsPrefersTopLevelCheckpointIDs(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-top-level-checkpoint", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{
			"thread_id":"thread-top-level-checkpoint",
			"checkpoint_id":"cp-stale",
			"parent_checkpoint_id":"cp-parent-stale"
		},
		"checkpoint_id":"cp-current",
		"parent_checkpoint_id":"cp-parent-current",
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-top-level-checkpoint"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Metadata["checkpoint_id"] != "cp-current" || session.Metadata["parent_checkpoint_id"] != "cp-parent-current" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	state := s.getThreadState("thread-top-level-checkpoint")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if state.CheckpointID != "cp-current" {
		t.Fatalf("checkpoint_id=%q", state.CheckpointID)
	}
	if state.ParentCheckpointID != "cp-parent-current" {
		t.Fatalf("parent_checkpoint_id=%q", state.ParentCheckpointID)
	}
}

func TestLoadPersistedThreadsPrefersTopLevelCheckpointObjects(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-top-level-checkpoint-objects", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{
			"thread_id":"thread-top-level-checkpoint-objects",
			"checkpoint_ns":"stale-ns",
			"checkpoint_thread_id":"stale-thread",
			"parent_checkpoint_ns":"stale-parent-ns",
			"parent_checkpoint_thread_id":"stale-parent-thread"
		},
		"checkpoint":{"checkpoint_id":"cp-current","thread_id":"thread-current","checkpoint_ns":"ns-current"},
		"parent_checkpoint":{"checkpoint_id":"cp-parent-current","thread_id":"thread-parent","checkpoint_ns":"ns-parent"},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-top-level-checkpoint-objects"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Metadata["checkpoint_ns"] != "ns-current" || session.Metadata["checkpoint_thread_id"] != "thread-current" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	if session.Metadata["parent_checkpoint_ns"] != "ns-parent" || session.Metadata["parent_checkpoint_thread_id"] != "thread-parent" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	state := s.getThreadState("thread-top-level-checkpoint-objects")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if state.Checkpoint == nil || state.Checkpoint["checkpoint_id"] != "cp-current" || state.Checkpoint["thread_id"] != "thread-current" || state.Checkpoint["checkpoint_ns"] != "ns-current" {
		t.Fatalf("checkpoint=%#v", state.Checkpoint)
	}
	if state.ParentCheckpoint == nil || state.ParentCheckpoint["checkpoint_id"] != "cp-parent-current" || state.ParentCheckpoint["thread_id"] != "thread-parent" || state.ParentCheckpoint["checkpoint_ns"] != "ns-parent" {
		t.Fatalf("parent_checkpoint=%#v", state.ParentCheckpoint)
	}
}

func TestLoadPersistedThreadsAcceptsCamelCaseParentCheckpointObject(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-parent-checkpoint-camel", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{"thread_id":"thread-parent-checkpoint-camel"},
		"parentCheckpoint":{"checkpointId":"cp-parent-current","threadId":"thread-parent","checkpointNs":"ns-parent"},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	state := s.getThreadState("thread-parent-checkpoint-camel")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if state.ParentCheckpoint == nil || state.ParentCheckpoint["checkpoint_id"] != "cp-parent-current" || state.ParentCheckpoint["thread_id"] != "thread-parent" || state.ParentCheckpoint["checkpoint_ns"] != "ns-parent" {
		t.Fatalf("parent_checkpoint=%#v", state.ParentCheckpoint)
	}
}

func TestLoadPersistedThreadsAcceptsTopLevelConfigurable(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-configurable-top-level", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello from state"}]},
		"configurable":{
			"threadId":"thread-configurable-top-level",
			"agentType":"deep_research",
			"agentName":"writer",
			"modelName":"deepseek/deepseek-r1",
			"reasoningEffort":"high",
			"thinkingEnabled":false,
			"isPlanMode":true,
			"subagentEnabled":true,
			"temperature":0.2,
			"maxTokens":321
		},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	state := s.getThreadState("thread-configurable-top-level")
	if state == nil {
		t.Fatalf("state=nil")
	}
	configurable, _ := state.Config["configurable"].(map[string]any)
	if configurable["thread_id"] != "thread-configurable-top-level" ||
		configurable["agent_type"] != "deep_research" ||
		configurable["agent_name"] != "writer" ||
		configurable["model_name"] != "deepseek/deepseek-r1" ||
		configurable["reasoning_effort"] != "high" {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("configurable=%#v", configurable)
	}
}

func TestLoadPersistedThreadsAcceptsFlatTopLevelConfig(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-flat-config", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello from state"}]},
		"threadId":"thread-flat-config",
		"agentType":"deep_research",
		"agentName":"writer",
		"modelName":"deepseek/deepseek-r1",
		"reasoningEffort":"high",
		"thinkingEnabled":false,
		"isPlanMode":true,
		"subagentEnabled":true,
		"temperature":0.2,
		"maxTokens":321,
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	state := s.getThreadState("thread-flat-config")
	if state == nil {
		t.Fatalf("state=nil")
	}
	configurable, _ := state.Config["configurable"].(map[string]any)
	if configurable["thread_id"] != "thread-flat-config" ||
		configurable["agent_type"] != "deep_research" ||
		configurable["agent_name"] != "writer" ||
		configurable["model_name"] != "deepseek/deepseek-r1" ||
		configurable["reasoning_effort"] != "high" {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("configurable=%#v", configurable)
	}
}

func TestLoadPersistedThreadsAcceptsFlatTopLevelMetadata(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-flat-metadata", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"messages":[{"id":"m1","type":"human","content":"hello from state"}],
		"threadId":"thread-flat-metadata",
		"assistantId":"assistant-1",
		"graphId":"graph-1",
		"runId":"run-1",
		"checkpointId":"cp-1",
		"parentCheckpointId":"cp-parent-1",
		"checkpointNs":"ns-1",
		"parentCheckpointNs":"ns-parent-1",
		"checkpointThreadId":"checkpoint-thread-1",
		"parentCheckpointThreadId":"checkpoint-thread-parent-1",
		"mode":"thinking",
		"temperature":0.2,
		"maxTokens":321,
		"step":7,
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	state := s.getThreadState("thread-flat-metadata")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if state.Metadata["assistant_id"] != "assistant-1" || state.Metadata["graph_id"] != "graph-1" || state.Metadata["run_id"] != "run-1" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["checkpoint_id"] != "cp-1" || state.Metadata["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["checkpoint_ns"] != "ns-1" || state.Metadata["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["checkpoint_thread_id"] != "checkpoint-thread-1" || state.Metadata["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["step"] != float64(7) && state.Metadata["step"] != 7 {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	configurable, _ := state.Config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("configurable=%#v", configurable)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("configurable=%#v", configurable)
	}
	if state.Checkpoint == nil || state.Checkpoint["checkpoint_id"] != "cp-1" || state.Checkpoint["checkpoint_ns"] != "ns-1" || state.Checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", state.Checkpoint)
	}
	if state.ParentCheckpoint == nil || state.ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || state.ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || state.ParentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", state.ParentCheckpoint)
	}
}

func TestLoadPersistedThreadsAcceptsTopLevelCompatValues(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-top-level-values", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"messages":[{"id":"m1","type":"human","content":"hello from state"}],
		"title":"Top Level Thread",
		"threadData":{"workspace_path":"/tmp/workspace","uploads_path":"/tmp/uploads","outputs_path":"/tmp/outputs"},
		"uploadedFiles":[{"filename":"notes.txt","path":"/tmp/uploads/notes.txt","status":"uploaded"}],
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	state := s.getThreadState("thread-top-level-values")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if state.Values["title"] != "Top Level Thread" {
		t.Fatalf("values=%#v", state.Values)
	}
	threadData, _ := state.Values["thread_data"].(map[string]any)
	if threadData["workspace_path"] != "/tmp/workspace" || threadData["uploads_path"] != "/tmp/uploads" || threadData["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("values=%#v", state.Values)
	}
	switch uploadedFiles := state.Values["uploaded_files"].(type) {
	case []map[string]any:
		if len(uploadedFiles) != 1 || uploadedFiles[0]["filename"] != "notes.txt" {
			t.Fatalf("values=%#v", state.Values)
		}
	case []any:
		if len(uploadedFiles) != 1 {
			t.Fatalf("values=%#v", state.Values)
		}
	default:
		t.Fatalf("values=%#v", state.Values)
	}
}

func TestLoadPersistedThreadsUsesCheckpointObjectsAsIDFallback(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-checkpoint-object-fallback", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{"thread_id":"thread-checkpoint-object-fallback"},
		"checkpoint":{"checkpoint_id":"cp-current","thread_id":"thread-current","checkpoint_ns":"ns-current"},
		"parent_checkpoint":{"checkpoint_id":"cp-parent-current","thread_id":"thread-parent","checkpoint_ns":"ns-parent"},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-checkpoint-object-fallback"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Metadata["checkpoint_id"] != "cp-current" || session.Metadata["parent_checkpoint_id"] != "cp-parent-current" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
}

func TestLoadPersistedThreadsPrefersCheckpointObjectsOverStaleMetadata(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-checkpoint-object-override", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{
			"thread_id":"thread-checkpoint-object-override",
			"checkpoint_id":"cp-stale",
			"parent_checkpoint_id":"cp-parent-stale"
		},
		"checkpoint":{"checkpoint_id":"cp-current","thread_id":"thread-current","checkpoint_ns":"ns-current"},
		"parent_checkpoint":{"checkpoint_id":"cp-parent-current","thread_id":"thread-parent","checkpoint_ns":"ns-parent"},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-checkpoint-object-override"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Metadata["checkpoint_id"] != "cp-current" || session.Metadata["parent_checkpoint_id"] != "cp-parent-current" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
}

func TestLoadPersistedThreadsPrefersValuesOverStaleMetadata(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-values-override", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{
			"messages":[{"id":"m1","type":"human","content":"hello"}],
			"title":"Current Title",
			"todos":[{"content":"current task","status":"completed"}],
			"artifacts":["/tmp/current.html"],
			"thread_data":{
				"workspace_path":"/external/thread/workspace",
				"uploads_path":"/external/thread/uploads",
				"outputs_path":"/external/thread/outputs"
			},
			"uploaded_files":[{"filename":"notes.txt","size":42,"path":"/mnt/user-data/uploads/notes.txt","status":"uploaded"}]
		},
		"metadata":{
			"thread_id":"thread-values-override",
			"title":"Stale Title",
			"todos":[{"content":"stale task","status":"pending"}],
			"artifacts":["/tmp/stale.html"]
		},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-values-override"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Metadata["title"] != "Current Title" {
		t.Fatalf("metadata=%#v", session.Metadata)
	}
	todos, _ := session.Metadata["todos"].([]any)
	if len(todos) != 1 {
		t.Fatalf("todos=%#v", session.Metadata["todos"])
	}
	todo, _ := todos[0].(map[string]any)
	if todo["content"] != "current task" {
		t.Fatalf("todo=%#v", todo)
	}
	if artifacts := anyStringSlice(session.Metadata["artifacts"]); len(artifacts) != 1 || artifacts[0] != "/tmp/current.html" {
		t.Fatalf("artifacts=%#v metadata=%#v", artifacts, session.Metadata)
	}
	state := s.getThreadState("thread-values-override")
	if state == nil {
		t.Fatalf("state=nil")
	}
	threadData, _ := state.Values["thread_data"].(map[string]any)
	if threadData["workspace_path"] != "/external/thread/workspace" || threadData["uploads_path"] != "/external/thread/uploads" || threadData["outputs_path"] != "/external/thread/outputs" {
		t.Fatalf("thread_data=%#v", state.Values["thread_data"])
	}
	if files, ok := state.Values["uploaded_files"].([]map[string]any); ok {
		if len(files) != 1 || files[0]["filename"] != "notes.txt" || files[0]["path"] != "/mnt/user-data/uploads/notes.txt" || toInt64(files[0]["size"]) != 42 {
			t.Fatalf("uploaded_files=%#v", files)
		}
	} else {
		rawFiles, _ := state.Values["uploaded_files"].([]any)
		if len(rawFiles) != 1 {
			t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
		}
		file, _ := rawFiles[0].(map[string]any)
		if file["filename"] != "notes.txt" || file["path"] != "/mnt/user-data/uploads/notes.txt" || toInt64(file["size"]) != 42 {
			t.Fatalf("uploaded_file=%#v", file)
		}
	}
}

func TestLoadPersistedThreadsDerivesBusyStatusFromNextTasks(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-busy-state", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{"thread_id":"thread-busy-state"},
		"next":["lead_agent"],
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-busy-state"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Status != "busy" {
		t.Fatalf("status=%q", session.Status)
	}
	state := s.getThreadState("thread-busy-state")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if len(state.Next) != 1 || state.Next[0] != "lead_agent" {
		t.Fatalf("next=%#v", state.Next)
	}
}

func TestLoadPersistedThreadsAcceptsScalarNext(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-scalar-next", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{"thread_id":"thread-scalar-next"},
		"next":"lead_agent",
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	state := s.getThreadState("thread-scalar-next")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if len(state.Next) != 1 || state.Next[0] != "lead_agent" {
		t.Fatalf("next=%#v", state.Next)
	}
}

func TestLoadPersistedThreadsAcceptsScalarTasksAndInterrupts(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-scalar-state-items", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{"thread_id":"thread-scalar-state-items"},
		"tasks":{"id":"task-1","name":"lead_agent"},
		"interrupts":{"value":"Need input"},
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	state := s.getThreadState("thread-scalar-state-items")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("tasks=%#v", state.Tasks)
	}
	task, _ := state.Tasks[0].(map[string]any)
	if task["id"] != "task-1" {
		t.Fatalf("task=%#v", task)
	}
	if len(state.Interrupts) != 1 {
		t.Fatalf("interrupts=%#v", state.Interrupts)
	}
	interrupt, _ := state.Interrupts[0].(map[string]any)
	if interrupt["value"] != "Need input" {
		t.Fatalf("interrupt=%#v", interrupt)
	}
}

func TestLoadPersistedThreadsDerivesInterruptedStatus(t *testing.T) {
	root := t.TempDir()
	threadDir := filepath.Join(root, "threads", "thread-interrupted-state", "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]},
		"metadata":{"thread_id":"thread-interrupted-state"},
		"interrupts":[{"value":"Need input"}],
		"tasks":[{"id":"task-1","name":"lead_agent"}],
		"created_at":"2026-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(threadDir, "thread.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write thread file: %v", err)
	}

	s := &Server{dataRoot: root, sessions: map[string]*Session{}}
	s.loadPersistedThreads()

	session := s.sessions["thread-interrupted-state"]
	if session == nil {
		t.Fatalf("sessions=%#v", s.sessions)
	}
	if session.Status != "interrupted" {
		t.Fatalf("status=%q", session.Status)
	}
	state := s.getThreadState("thread-interrupted-state")
	if state == nil {
		t.Fatalf("state=nil")
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("tasks=%#v", state.Tasks)
	}
	if len(state.Interrupts) != 1 {
		t.Fatalf("interrupts=%#v", state.Interrupts)
	}
	metadataInterrupts, _ := state.Metadata["interrupts"].([]any)
	if len(metadataInterrupts) != 1 {
		t.Fatalf("interrupts=%#v", state.Metadata["interrupts"])
	}
}

func TestLoadPersistedRunsAcceptsCamelCaseFields(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"runId":"run-camel",
		"threadId":"thread-camel",
		"assistantId":"lead_agent",
		"status":"success",
		"createdAt":"2026-01-01T00:00:00Z",
		"updatedAt":"2026-01-02T00:00:00Z",
		"events":[],
		"error":""
	}`
	if err := os.WriteFile(filepath.Join(runDir, "run-camel.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write run file: %v", err)
	}

	s := &Server{dataRoot: root, runs: map[string]*Run{}}
	s.loadPersistedRuns()

	run, ok := s.runs["run-camel"]
	if !ok {
		t.Fatalf("missing run: %#v", s.runs)
	}
	if run.ThreadID != "thread-camel" || run.AssistantID != "lead_agent" {
		t.Fatalf("run=%#v", run)
	}
	if run.CreatedAt.IsZero() || run.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not loaded: %#v", run)
	}
}

func TestLoadPersistedRunsNormalizesCamelCaseEventFields(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"runId":"run-events-camel",
		"threadId":"thread-camel",
		"assistantId":"lead_agent",
		"status":"success",
		"createdAt":"2026-01-01T00:00:00Z",
		"updatedAt":"2026-01-02T00:00:00Z",
		"events":[
			{
				"id":"evt-1",
				"event":"messages-tuple",
				"data":{"type":"ai","role":"assistant","content":"done"},
				"runId":"run-events-camel",
				"threadId":"thread-camel"
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(runDir, "run-events-camel.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write run file: %v", err)
	}

	s := &Server{dataRoot: root, runs: map[string]*Run{}}
	s.loadPersistedRuns()

	run, ok := s.runs["run-events-camel"]
	if !ok {
		t.Fatalf("missing run: %#v", s.runs)
	}
	if len(run.Events) != 1 {
		t.Fatalf("events=%#v", run.Events)
	}
	event := run.Events[0]
	if event.ID != "evt-1" || event.Event != "messages-tuple" {
		t.Fatalf("event=%#v", event)
	}
	data, ok := event.Data.(map[string]any)
	if !ok || data["content"] != "done" {
		t.Fatalf("data=%#v", event.Data)
	}
	if event.RunID != "run-events-camel" || event.ThreadID != "thread-camel" {
		t.Fatalf("event=%#v", event)
	}
}

func TestLoadPersistedRunsAcceptsWrappedRunObject(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"run":{
			"runId":"run-wrapped",
			"threadId":"thread-wrapped",
			"assistantId":"lead_agent",
			"status":"success",
			"createdAt":"2026-01-01T00:00:00Z",
			"updatedAt":"2026-01-02T00:00:00Z",
			"events":[{"id":"evt-1","event":"end","data":{"ok":true},"runId":"run-wrapped","threadId":"thread-wrapped"}]
		}
	}`
	if err := os.WriteFile(filepath.Join(runDir, "run-wrapped.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write run file: %v", err)
	}

	s := &Server{dataRoot: root, runs: map[string]*Run{}}
	s.loadPersistedRuns()

	run := s.runs["run-wrapped"]
	if run == nil {
		t.Fatalf("runs=%#v", s.runs)
	}
	if run.ThreadID != "thread-wrapped" || run.AssistantID != "lead_agent" || len(run.Events) != 1 || run.Events[0].Event != "end" {
		t.Fatalf("run=%#v", run)
	}
}

func TestLoadPersistedRunsAcceptsDataWrapper(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"data":{
			"runId":"run-data-wrap",
			"threadId":"thread-data-wrap",
			"assistantId":"lead_agent",
			"status":"success",
			"createdAt":"2026-01-01T00:00:00Z",
			"updatedAt":"2026-01-02T00:00:00Z",
			"events":[{"id":"evt-1","event":"end","data":{"ok":true},"runId":"run-data-wrap","threadId":"thread-data-wrap"}]
		}
	}`
	if err := os.WriteFile(filepath.Join(runDir, "run-data-wrap.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write run file: %v", err)
	}
	s := &Server{dataRoot: root, runs: map[string]*Run{}}
	s.loadPersistedRuns()
	if run := s.runs["run-data-wrap"]; run == nil || run.ThreadID != "thread-data-wrap" || len(run.Events) != 1 {
		t.Fatalf("runs=%#v", s.runs)
	}
}

func TestLoadThreadHistoryAcceptsCamelCaseFields(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-camel"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpoint_id":"cp-1",
			"values":{"title":"History"},
			"metadata":{"thread_id":"thread-history-camel"},
			"createdAt":"2026-01-01T00:00:00Z"
		}
	]`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("len=%d want 1", len(history))
	}
	if history[0].CreatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("created_at=%q", history[0].CreatedAt)
	}
	if history[0].CheckpointID != "cp-1" {
		t.Fatalf("checkpoint_id=%q", history[0].CheckpointID)
	}
}

func TestLoadThreadHistoryAcceptsCamelCaseCheckpointID(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-checkpoint-camel"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-camel",
			"values":{"title":"History"}
		}
	]`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("len=%d want 1", len(history))
	}
	if history[0].CheckpointID != "cp-camel" {
		t.Fatalf("checkpoint_id=%q", history[0].CheckpointID)
	}
}

func TestLoadThreadHistoryNormalizesCamelCaseMetadata(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-meta"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"values":{"title":"History"},
			"metadata":{
				"threadId":"thread-history-meta",
				"assistantId":"assistant-1",
				"graphId":"graph-1",
				"runId":"run-1",
				"viewedImages":{"a":{"base64":"x"}}
			},
			"createdAt":"2026-01-01T00:00:00Z"
		}
	]`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	if history[0].Metadata["thread_id"] != "thread-history-meta" || history[0].Metadata["assistant_id"] != "assistant-1" || history[0].Metadata["graph_id"] != "graph-1" || history[0].Metadata["run_id"] != "run-1" {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
	if _, ok := history[0].Metadata["viewed_images"]; !ok {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
}

func TestLoadThreadHistoryNormalizesCamelCaseValues(t *testing.T) {
	root := t.TempDir()
	threadID := "thread-history-values-camel"
	threadDir := filepath.Join(root, "threads", threadID, "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"createdAt":"2026-01-01T00:00:00Z",
			"values":{
				"viewedImages":{"a":{"base64":"x"}},
				"threadData":{"workspace_path":"/tmp/workspace","uploads_path":"/tmp/uploads","outputs_path":"/tmp/outputs"},
				"uploadedFiles":[{"filename":"notes.txt","path":"/tmp/uploads/notes.txt","status":"uploaded"}]
			}
		}
	]`
	if err := os.WriteFile(filepath.Join(threadDir, "thread_history.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history: %v", err)
	}

	s := &Server{dataRoot: root}
	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%d", len(history))
	}
	viewedImages, _ := history[0].Values["viewed_images"].(map[string]any)
	if len(viewedImages) != 1 {
		t.Fatalf("values=%#v", history[0].Values)
	}
	threadData, _ := history[0].Values["thread_data"].(map[string]any)
	if threadData["workspace_path"] != "/tmp/workspace" || threadData["uploads_path"] != "/tmp/uploads" || threadData["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("values=%#v", history[0].Values)
	}
	switch uploadedFiles := history[0].Values["uploaded_files"].(type) {
	case []any:
		if len(uploadedFiles) != 1 {
			t.Fatalf("values=%#v", history[0].Values)
		}
	case []map[string]any:
		if len(uploadedFiles) != 1 || uploadedFiles[0]["filename"] != "notes.txt" {
			t.Fatalf("values=%#v", history[0].Values)
		}
	default:
		t.Fatalf("values=%#v", history[0].Values)
	}
}

func TestLoadThreadHistoryNormalizesCamelCaseConfig(t *testing.T) {
	root := t.TempDir()
	threadID := "thread-history-config-camel"
	threadDir := filepath.Join(root, "threads", threadID, "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"createdAt":"2026-01-01T00:00:00Z",
			"config":{
				"configurable":{
					"threadId":"thread-history-config-camel",
					"agentType":"deep_research",
					"agentName":"writer",
					"modelName":"deepseek/deepseek-r1",
					"reasoningEffort":"high",
					"thinkingEnabled":false,
					"isPlanMode":true,
					"subagentEnabled":true,
					"temperature":0.2,
					"maxTokens":321
				}
			}
		}
	]`
	if err := os.WriteFile(filepath.Join(threadDir, "thread_history.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history: %v", err)
	}

	s := &Server{dataRoot: root}
	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%d", len(history))
	}
	configurable, _ := history[0].Config["configurable"].(map[string]any)
	if configurable["thread_id"] != "thread-history-config-camel" ||
		configurable["agent_type"] != "deep_research" ||
		configurable["agent_name"] != "writer" ||
		configurable["model_name"] != "deepseek/deepseek-r1" ||
		configurable["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v", history[0].Config)
	}
}

func TestLoadThreadHistoryAcceptsTopLevelConfigurable(t *testing.T) {
	root := t.TempDir()
	threadID := "thread-history-configurable-top-level"
	threadDir := filepath.Join(root, "threads", threadID, "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"createdAt":"2026-01-01T00:00:00Z",
			"configurable":{
				"threadId":"thread-history-configurable-top-level",
				"modelName":"deepseek/deepseek-r1",
				"temperature":0.2,
				"maxTokens":321
			}
		}
	]`
	if err := os.WriteFile(filepath.Join(threadDir, "thread_history.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history: %v", err)
	}

	s := &Server{dataRoot: root}
	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%d", len(history))
	}
	configurable, _ := history[0].Config["configurable"].(map[string]any)
	if configurable["thread_id"] != "thread-history-configurable-top-level" || configurable["model_name"] != "deepseek/deepseek-r1" {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != 321 && configurable["max_tokens"] != int64(321) {
		t.Fatalf("config=%#v", history[0].Config)
	}
}

func TestLoadThreadHistoryAcceptsTopLevelCompatValues(t *testing.T) {
	root := t.TempDir()
	threadID := "thread-history-top-level-values"
	threadDir := filepath.Join(root, "threads", threadID, "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"createdAt":"2026-01-01T00:00:00Z",
			"title":"Top Level History",
			"threadData":{"workspace_path":"/tmp/workspace","uploads_path":"/tmp/uploads","outputs_path":"/tmp/outputs"},
			"uploadedFiles":[{"filename":"notes.txt","path":"/tmp/uploads/notes.txt","status":"uploaded"}]
		}
	]`
	if err := os.WriteFile(filepath.Join(threadDir, "thread_history.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history: %v", err)
	}

	s := &Server{dataRoot: root}
	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%d", len(history))
	}
	if history[0].Values["title"] != "Top Level History" {
		t.Fatalf("values=%#v", history[0].Values)
	}
	threadData, _ := history[0].Values["thread_data"].(map[string]any)
	if threadData["workspace_path"] != "/tmp/workspace" || threadData["uploads_path"] != "/tmp/uploads" || threadData["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("values=%#v", history[0].Values)
	}
	switch uploadedFiles := history[0].Values["uploaded_files"].(type) {
	case []map[string]any:
		if len(uploadedFiles) != 1 || uploadedFiles[0]["filename"] != "notes.txt" {
			t.Fatalf("values=%#v", history[0].Values)
		}
	case []any:
		if len(uploadedFiles) != 1 {
			t.Fatalf("values=%#v", history[0].Values)
		}
	default:
		t.Fatalf("values=%#v", history[0].Values)
	}
}

func TestLoadThreadHistoryAcceptsFlatTopLevelConfig(t *testing.T) {
	root := t.TempDir()
	threadID := "thread-history-flat-config"
	threadDir := filepath.Join(root, "threads", threadID, "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"createdAt":"2026-01-01T00:00:00Z",
			"threadId":"thread-history-flat-config",
			"modelName":"deepseek/deepseek-r1",
			"agentType":"deep_research",
			"temperature":0.2,
			"maxTokens":321
		}
	]`
	if err := os.WriteFile(filepath.Join(threadDir, "thread_history.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history: %v", err)
	}

	s := &Server{dataRoot: root}
	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%d", len(history))
	}
	configurable, _ := history[0].Config["configurable"].(map[string]any)
	if configurable["thread_id"] != "thread-history-flat-config" || configurable["model_name"] != "deepseek/deepseek-r1" || configurable["agent_type"] != "deep_research" {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != 321 && configurable["max_tokens"] != int64(321) {
		t.Fatalf("config=%#v", history[0].Config)
	}
}

func TestLoadThreadHistoryAcceptsFlatTopLevelMetadata(t *testing.T) {
	root := t.TempDir()
	threadID := "thread-history-flat-metadata"
	threadDir := filepath.Join(root, "threads", threadID, "user-data")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"parentCheckpointId":"cp-parent-1",
			"checkpointNs":"ns-1",
			"parentCheckpointNs":"ns-parent-1",
			"checkpointThreadId":"checkpoint-thread-1",
			"parentCheckpointThreadId":"checkpoint-thread-parent-1",
			"createdAt":"2026-01-01T00:00:00Z",
			"threadId":"thread-history-flat-metadata",
			"assistantId":"assistant-1",
			"graphId":"graph-1",
			"runId":"run-1",
			"mode":"thinking",
			"temperature":0.2,
			"maxTokens":321,
			"step":7
		}
	]`
	if err := os.WriteFile(filepath.Join(threadDir, "thread_history.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history: %v", err)
	}

	s := &Server{dataRoot: root}
	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%d", len(history))
	}
	if history[0].Metadata["assistant_id"] != "assistant-1" || history[0].Metadata["graph_id"] != "graph-1" || history[0].Metadata["run_id"] != "run-1" {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
	if history[0].Metadata["checkpoint_id"] != "cp-1" || history[0].Metadata["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
	if history[0].Metadata["checkpoint_ns"] != "ns-1" || history[0].Metadata["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
	if history[0].Metadata["checkpoint_thread_id"] != "checkpoint-thread-1" || history[0].Metadata["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
	if history[0].Metadata["step"] != float64(7) && history[0].Metadata["step"] != 7 {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
	configurable, _ := history[0].Config["configurable"].(map[string]any)
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v metadata=%#v", history[0].Config, history[0].Metadata)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v metadata=%#v", history[0].Config, history[0].Metadata)
	}
	if history[0].Checkpoint == nil || history[0].Checkpoint["checkpoint_id"] != "cp-1" || history[0].Checkpoint["checkpoint_ns"] != "ns-1" || history[0].Checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v metadata=%#v", history[0].Checkpoint, history[0].Metadata)
	}
	if history[0].ParentCheckpoint == nil || history[0].ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || history[0].ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || history[0].ParentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v metadata=%#v", history[0].ParentCheckpoint, history[0].Metadata)
	}
}

func TestLoadThreadHistoryNormalizesCheckpointObjects(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-checkpoint-objects"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"checkpoint":{"checkpointId":"cp-1","checkpointNs":"ns-1","threadId":"thread-1"},
			"parent_checkpoint":{"checkpointId":"cp-parent-1","checkpointNs":"ns-parent-1","threadId":"thread-parent-1"},
			"metadata":{"threadId":"thread-history-checkpoint-objects"},
			"createdAt":"2026-01-01T00:00:00Z"
		}
	]`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	if history[0].Checkpoint == nil || history[0].Checkpoint["checkpoint_id"] != "cp-1" || history[0].Checkpoint["checkpoint_ns"] != "ns-1" || history[0].Checkpoint["thread_id"] != "thread-1" {
		t.Fatalf("checkpoint=%#v", history[0].Checkpoint)
	}
	if history[0].ParentCheckpoint == nil || history[0].ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || history[0].ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || history[0].ParentCheckpoint["thread_id"] != "thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", history[0].ParentCheckpoint)
	}
	if history[0].ParentCheckpointID != "cp-parent-1" {
		t.Fatalf("parent_checkpoint_id=%q", history[0].ParentCheckpointID)
	}
}

func TestLoadThreadHistoryAcceptsCamelCaseParentCheckpointObject(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-parent-checkpoint-camel"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"parentCheckpoint":{"checkpointId":"cp-parent-1","checkpointNs":"ns-parent-1","threadId":"thread-parent-1"},
			"createdAt":"2026-01-01T00:00:00Z"
		}
	]`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	if history[0].ParentCheckpoint == nil || history[0].ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || history[0].ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || history[0].ParentCheckpoint["thread_id"] != "thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", history[0].ParentCheckpoint)
	}
	if history[0].ParentCheckpointID != "cp-parent-1" {
		t.Fatalf("parent_checkpoint_id=%q", history[0].ParentCheckpointID)
	}
}

func TestLoadThreadHistoryUsesCheckpointObjectAsIDFallback(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-checkpoint-fallback"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpoint":{"checkpointId":"cp-1","checkpointNs":"ns-1","threadId":"thread-1"},
			"createdAt":"2026-01-01T00:00:00Z"
		}
	]`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	if history[0].CheckpointID != "cp-1" {
		t.Fatalf("checkpoint_id=%q checkpoint=%#v", history[0].CheckpointID, history[0].Checkpoint)
	}
	if history[0].ParentCheckpointID != "" {
		t.Fatalf("parent_checkpoint_id=%q", history[0].ParentCheckpointID)
	}
}

func TestLoadThreadHistoryAcceptsScalarStateItems(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-scalar-state"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `[
		{
			"checkpointId":"cp-1",
			"next":"lead_agent",
			"tasks":{"id":"task-1","name":"lead_agent"},
			"interrupts":{"value":"Need input"},
			"createdAt":"2026-01-01T00:00:00Z"
		}
	]`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	if len(history[0].Next) != 1 || history[0].Next[0] != "lead_agent" {
		t.Fatalf("next=%#v", history[0].Next)
	}
	if len(history[0].Tasks) != 1 {
		t.Fatalf("tasks=%#v", history[0].Tasks)
	}
	task, _ := history[0].Tasks[0].(map[string]any)
	if task["id"] != "task-1" {
		t.Fatalf("task=%#v", task)
	}
	if len(history[0].Interrupts) != 1 {
		t.Fatalf("interrupts=%#v", history[0].Interrupts)
	}
	interrupt, _ := history[0].Interrupts[0].(map[string]any)
	if interrupt["value"] != "Need input" {
		t.Fatalf("interrupt=%#v", interrupt)
	}
}

func TestLoadThreadHistoryAcceptsWrappedHistoryObject(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-wrapped"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"history":[
			{
				"checkpointId":"cp-1",
				"values":{"title":"History"},
				"metadata":{"threadId":"thread-history-wrapped"},
				"createdAt":"2026-01-01T00:00:00Z"
			}
		]
	}`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	if history[0].CheckpointID != "cp-1" || history[0].CreatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("history=%#v", history[0])
	}
	if history[0].Metadata["thread_id"] != "thread-history-wrapped" {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
}

func TestLoadThreadHistoryAcceptsWrappedItemsObject(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-items"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"items":[
			{
				"checkpointId":"cp-1",
				"values":{"title":"History"},
				"metadata":{"threadId":"thread-history-items"},
				"createdAt":"2026-01-01T00:00:00Z"
			}
		]
	}`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}

	history := s.loadThreadHistory(threadID)
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	if history[0].CheckpointID != "cp-1" || history[0].CreatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("history=%#v", history[0])
	}
	if history[0].Metadata["thread_id"] != "thread-history-items" {
		t.Fatalf("metadata=%#v", history[0].Metadata)
	}
}

func TestLoadThreadHistoryAcceptsDataWrapper(t *testing.T) {
	root := t.TempDir()
	s := &Server{dataRoot: root}
	threadID := "thread-history-data"
	if err := os.MkdirAll(s.threadRoot(threadID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{
		"data":[
			{
				"checkpointId":"cp-1",
				"values":{"title":"History"},
				"metadata":{"threadId":"thread-history-data"},
				"createdAt":"2026-01-01T00:00:00Z"
			}
		]
	}`
	if err := os.WriteFile(s.threadHistoryPath(threadID), []byte(raw), 0o644); err != nil {
		t.Fatalf("write history file: %v", err)
	}
	history := s.loadThreadHistory(threadID)
	if len(history) != 1 || history[0].Metadata["thread_id"] != "thread-history-data" {
		t.Fatalf("history=%#v", history)
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

func TestAgentCreateAcceptsCamelCaseToolGroups(t *testing.T) {
	_, ts := newCompatTestServer(t)

	body := `{"name":"camel-writer","description":"Writes clearly","model":"qwen/Qwen3.5-9B","toolGroups":["file"],"soul":"Write with structure."}`
	resp, err := http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatalf("decode agent: %v", err)
	}
	toolGroups, ok := agent["tool_groups"].([]any)
	if !ok || len(toolGroups) != 1 || toolGroups[0] != "file" {
		t.Fatalf("tool_groups=%#v", agent["tool_groups"])
	}
}

func TestAgentUpdateAcceptsCamelCaseToolGroups(t *testing.T) {
	_, ts := newCompatTestServer(t)

	createBody := `{"name":"camel-updater","description":"a","model":"qwen/Qwen3.5-9B","tool_groups":["file"],"soul":"hello"}`
	createResp, err := http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	createResp.Body.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/agents/camel-updater", strings.NewReader(`{"toolGroups":["web","file"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update agent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatalf("decode agent: %v", err)
	}
	toolGroups, ok := agent["tool_groups"].([]any)
	if !ok || len(toolGroups) != 2 || toolGroups[0] != "web" || toolGroups[1] != "file" {
		t.Fatalf("tool_groups=%#v", agent["tool_groups"])
	}
}

func TestAgentUpdateAcceptsNullClearsFields(t *testing.T) {
	s, ts := newCompatTestServer(t)

	createBody := `{"name":"null-updater","description":"a","model":"qwen/Qwen3.5-9B","tool_groups":["file"],"soul":"hello"}`
	createResp, err := http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	createResp.Body.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/agents/null-updater", strings.NewReader(`{"description":null,"model":null,"tool_groups":null,"soul":null}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update agent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatalf("decode agent: %v", err)
	}
	if agent["description"] != "" {
		t.Fatalf("description=%#v", agent["description"])
	}
	if value, ok := agent["model"]; ok && value != nil {
		t.Fatalf("model=%#v", value)
	}
	if value, ok := agent["tool_groups"]; ok && value != nil {
		t.Fatalf("tool_groups=%#v", value)
	}
	if _, ok := agent["soul"]; ok {
		t.Fatalf("expected soul omitted after clear, got %#v", agent["soul"])
	}

	s.uiStateMu.RLock()
	persisted := s.getAgentsLocked()["null-updater"]
	s.uiStateMu.RUnlock()
	if persisted.Description != "" || persisted.Model != nil || persisted.ToolGroups != nil || persisted.Soul != "" {
		t.Fatalf("persisted agent=%#v", persisted)
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

func TestDefaultGatewayMemoryConfigHonorsExplicitZeroValues(t *testing.T) {
	t.Setenv("DEERFLOW_MEMORY_CONFIG", `{"enabled":true,"debounce_seconds":0,"max_facts":0,"fact_confidence_threshold":0,"injection_enabled":false,"max_injection_tokens":0}`)
	cfg := defaultGatewayMemoryConfig("/tmp/deerflow-test")
	if cfg.DebounceSeconds != 0 {
		t.Fatalf("debounce_seconds=%d want 0", cfg.DebounceSeconds)
	}
	if cfg.MaxFacts != 0 {
		t.Fatalf("max_facts=%d want 0", cfg.MaxFacts)
	}
	if cfg.FactConfidenceThreshold != 0 {
		t.Fatalf("fact_confidence_threshold=%v want 0", cfg.FactConfidenceThreshold)
	}
	if cfg.MaxInjectionTokens != 0 {
		t.Fatalf("max_injection_tokens=%d want 0", cfg.MaxInjectionTokens)
	}
}

func TestDefaultGatewayMemoryConfigAcceptsCamelCaseFields(t *testing.T) {
	t.Setenv("DEERFLOW_MEMORY_CONFIG", `{"enabled":true,"storagePath":"/tmp/custom-memory.json","debounceSeconds":12,"maxFacts":22,"factConfidenceThreshold":0.5,"injectionEnabled":false,"maxInjectionTokens":321}`)
	cfg := defaultGatewayMemoryConfig("/tmp/deerflow-test")
	if cfg.StoragePath != "/tmp/custom-memory.json" {
		t.Fatalf("storage_path=%q want /tmp/custom-memory.json", cfg.StoragePath)
	}
	if cfg.DebounceSeconds != 12 || cfg.MaxFacts != 22 {
		t.Fatalf("unexpected cfg: %#v", cfg)
	}
	if cfg.FactConfidenceThreshold != 0.5 {
		t.Fatalf("fact_confidence_threshold=%v want 0.5", cfg.FactConfidenceThreshold)
	}
	if cfg.InjectionEnabled {
		t.Fatalf("expected injection disabled: %#v", cfg)
	}
	if cfg.MaxInjectionTokens != 321 {
		t.Fatalf("max_injection_tokens=%d want 321", cfg.MaxInjectionTokens)
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

func TestDefaultGatewayChannelsStatusFromEnvAcceptsCamelCase(t *testing.T) {
	t.Setenv("DEERFLOW_CHANNELS_CONFIG", `{"serviceRunning":true,"channels":{"telegram":{"enabled":true,"connected":false}}}`)
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

func TestThreadHistoryAcceptsPageSizeAlias(t *testing.T) {
	_, ts := newCompatTestServer(t)
	resp, err := http.Post(ts.URL+"/threads", "application/json", strings.NewReader(`{"thread_id":"history-pagesize"}`))
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	resp.Body.Close()

	for i := 1; i <= 3; i++ {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/history-pagesize/state", strings.NewReader(fmt.Sprintf(`{"values":{"title":"Version %d"}}`, i)))
		req.Header.Set("Content-Type", "application/json")
		stateResp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("update state: %v", err)
		}
		stateResp.Body.Close()
	}

	historyReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/history-pagesize/history", strings.NewReader(`{"pageSize":2}`))
	historyReq.Header.Set("Content-Type", "application/json")
	historyResp, err := http.DefaultClient.Do(historyReq)
	if err != nil {
		t.Fatalf("history request: %v", err)
	}
	defer historyResp.Body.Close()
	var history []ThreadState
	if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len=%d want 2", len(history))
	}
}

func TestThreadHistoryGetAcceptsLimitQuery(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("history-query", nil)

	for i := 1; i <= 3; i++ {
		body := strings.NewReader(fmt.Sprintf(`{"values":{"title":"Version %d"}}`, i))
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/history-query/state", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post thread state: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("state status=%d", resp.StatusCode)
		}
	}

	resp, err := http.Get(ts.URL + "/threads/history-query/history?limit=2")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", resp.StatusCode)
	}

	var history []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len=%d", len(history))
	}
}

func TestThreadHistoryReturnsLatestSnapshotFirst(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.ensureSession("history-latest", nil)

	for i := 1; i <= 3; i++ {
		body := strings.NewReader(fmt.Sprintf(`{"values":{"title":"Version %d"}}`, i))
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/history-latest/state", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post thread state: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("state status=%d", resp.StatusCode)
		}
	}

	resp, err := http.Get(ts.URL + "/threads/history-latest/history?limit=1")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", resp.StatusCode)
	}

	var history []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history len=%d want 1", len(history))
	}
	values, ok := history[0]["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", history[0]["values"])
	}
	if values["title"] != "Version 3" {
		t.Fatalf("title=%v want Version 3", values["title"])
	}
}

func TestThreadHistoryIncludesRecoveredStateShape(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "history-shape"
	session := s.ensureSession(threadID, map[string]any{
		"title":                       "Snapshot",
		"mode":                        "thinking",
		"model_name":                  "deepseek/deepseek-r1",
		"reasoning_effort":            "high",
		"thinking_enabled":            false,
		"is_plan_mode":                true,
		"subagent_enabled":            true,
		"temperature":                 0.2,
		"max_tokens":                  321,
		"checkpoint_id":               "cp-1",
		"parent_checkpoint_id":        "cp-parent-1",
		"checkpoint_ns":               "ns-1",
		"parent_checkpoint_ns":        "ns-parent-1",
		"checkpoint_thread_id":        "checkpoint-thread-1",
		"parent_checkpoint_thread_id": "checkpoint-thread-parent-1",
		"next":                        []any{"lead_agent"},
		"tasks":                       []any{map[string]any{"id": "task-1", "name": "lead_agent"}},
		"interrupts":                  []any{map[string]any{"value": "Need input"}},
	})
	if err := s.persistSessionFile(session); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	if err := s.appendThreadHistorySnapshot(threadID); err != nil {
		t.Fatalf("append history: %v", err)
	}

	resp, err := http.Get(ts.URL + "/threads/" + threadID + "/history?limit=1")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", resp.StatusCode)
	}

	var history []ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history len=%d", len(history))
	}
	configurable, _ := history[0].Config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" || configurable["model_name"] != "deepseek/deepseek-r1" || configurable["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != 321 && configurable["max_tokens"] != int64(321) {
		t.Fatalf("config=%#v", history[0].Config)
	}
	if history[0].Checkpoint == nil || history[0].Checkpoint["checkpoint_id"] != "cp-1" || history[0].Checkpoint["checkpoint_ns"] != "ns-1" || history[0].Checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", history[0].Checkpoint)
	}
	if history[0].ParentCheckpoint == nil || history[0].ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || history[0].ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || history[0].ParentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", history[0].ParentCheckpoint)
	}
	if len(history[0].Next) != 1 || history[0].Next[0] != "lead_agent" {
		t.Fatalf("next=%#v", history[0].Next)
	}
	if len(history[0].Tasks) != 1 {
		t.Fatalf("tasks=%#v", history[0].Tasks)
	}
	if len(history[0].Interrupts) != 1 {
		t.Fatalf("interrupts=%#v", history[0].Interrupts)
	}
}

func TestThreadHistoryPreservesMessageCompatShape(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-history-message-shape"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{
		{
			ID:        "ai-1",
			SessionID: threadID,
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
				"reasoning_content":   "Need to inspect report",
				"usage_input_tokens":  "10",
				"usage_output_tokens": "5",
				"usage_total_tokens":  "15",
			},
		},
		{
			ID:        "tool-1",
			SessionID: threadID,
			Role:      models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "call-1",
				ToolName: "present_files",
				Status:   models.CallStatusCompleted,
				Content:  "presented",
				Duration: 1500 * time.Millisecond,
				Data: map[string]any{
					"files": []string{"/tmp/report.md"},
				},
			},
			Metadata: map[string]string{
				"message_status": "success",
			},
		},
	}
	if err := s.persistSessionFile(session); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	if err := s.appendThreadHistorySnapshot(threadID); err != nil {
		t.Fatalf("append history: %v", err)
	}

	resp, err := http.Get(ts.URL + "/threads/" + threadID + "/history?limit=1")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", resp.StatusCode)
	}

	var history []ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history len=%d", len(history))
	}

	messages, _ := history[0].Values["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("values=%#v", history[0].Values)
	}
	ai, _ := messages[0].(map[string]any)
	if ai["id"] != "ai-1" {
		t.Fatalf("message=%#v", ai)
	}
	toolCalls, _ := ai["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("message=%#v", ai)
	}
	additionalKwargs, _ := ai["additional_kwargs"].(map[string]any)
	if additionalKwargs["reasoning_content"] != "Need to inspect report" {
		t.Fatalf("additional_kwargs=%#v", additionalKwargs)
	}
	usage, _ := ai["usage_metadata"].(map[string]any)
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) || usage["total_tokens"] != float64(15) {
		t.Fatalf("usage_metadata=%#v", usage)
	}
	tool, _ := messages[1].(map[string]any)
	if tool["id"] != "tool-1" || tool["tool_call_id"] != "call-1" || tool["status"] != "success" {
		t.Fatalf("message=%#v", tool)
	}
	data, _ := tool["data"].(map[string]any)
	if data["duration"] != "1.5s" {
		t.Fatalf("data=%#v", data)
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

func TestThreadRunResponsesIncludeError(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-error-1",
		ThreadID:    "thread-error-1",
		AssistantID: "lead_agent",
		Status:      "error",
		Error:       "boom",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	s.saveRun(run)

	listResp, err := http.Get(ts.URL + "/threads/thread-error-1/runs")
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
	if len(listData.Runs) != 1 || listData.Runs[0]["error"] != "boom" {
		t.Fatalf("unexpected runs payload: %#v", listData.Runs)
	}

	getResp, err := http.Get(ts.URL + "/threads/thread-error-1/runs/run-error-1")
	if err != nil {
		t.Fatalf("get scoped run: %v", err)
	}
	defer getResp.Body.Close()
	var getData map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&getData); err != nil {
		t.Fatalf("decode scoped run: %v", err)
	}
	if getData["error"] != "boom" {
		t.Fatalf("unexpected run payload: %#v", getData)
	}

	globalResp, err := http.Get(ts.URL + "/runs/run-error-1")
	if err != nil {
		t.Fatalf("get global run: %v", err)
	}
	defer globalResp.Body.Close()
	var globalData map[string]any
	if err := json.NewDecoder(globalResp.Body).Decode(&globalData); err != nil {
		t.Fatalf("decode global run: %v", err)
	}
	if globalData["error"] != "boom" {
		t.Fatalf("unexpected global run payload: %#v", globalData)
	}
}

func TestThreadRunsCreateReturnsFinalState(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"id":"user-1","role":"user","content":"hi"}]}}`
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
	if !ok || len(messages) < 2 {
		t.Fatalf("messages missing: %#v", payload)
	}
	first, _ := messages[0].(map[string]any)
	last, _ := messages[len(messages)-1].(map[string]any)
	if first["id"] != "user-1" || first["content"] != "hi" {
		t.Fatalf("first message=%#v", first)
	}
	if last["content"] != "hello from fake llm" {
		t.Fatalf("last message=%#v", last)
	}
}

func TestThreadRunsCreateAcceptsTopLevelMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","messages":[{"id":"user-top","role":"user","content":"hi from top level"}]}`
	resp, err := http.Post(ts.URL+"/threads/thread-top-level-run/runs", "application/json", strings.NewReader(body))
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
	if payload["thread_id"] != "thread-top-level-run" {
		t.Fatalf("thread_id=%v", payload["thread_id"])
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("messages missing: %#v", payload)
	}
	first, _ := messages[0].(map[string]any)
	if first["id"] != "user-top" || first["content"] != "hi from top level" {
		t.Fatalf("first message=%#v", first)
	}
}

func TestThreadRunsCreatePersistsMessagesAcrossReload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"id":"user-1","role":"user","content":"hi"}]}}`
	resp, err := http.Post(ts.URL+"/threads/thread-run-persist/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	loaded := &Server{
		dataRoot: s.dataRoot,
		sessions: map[string]*Session{},
	}
	loaded.loadPersistedThreads()

	session := loaded.sessions["thread-run-persist"]
	if session == nil {
		t.Fatal("expected thread restored")
	}
	if len(session.Messages) < 2 {
		t.Fatalf("messages=%#v", session.Messages)
	}
	if session.Messages[0].ID != "user-1" || session.Messages[0].Content != "hi" {
		t.Fatalf("message=%#v", session.Messages[0])
	}
	if session.Messages[len(session.Messages)-1].Content != "hello from fake llm" {
		t.Fatalf("last message=%#v", session.Messages[len(session.Messages)-1])
	}
}

func TestThreadRunsCreateUpdatesHistoryMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"id":"user-1","role":"user","content":"hi"}]}}`
	resp, err := http.Post(ts.URL+"/threads/thread-run-history/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	historyResp, err := http.Get(ts.URL + "/threads/thread-run-history/history?limit=1")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer historyResp.Body.Close()
	if historyResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(historyResp.Body)
		t.Fatalf("status=%d body=%s", historyResp.StatusCode, string(b))
	}

	var history []ThreadState
	if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history len=%d", len(history))
	}
	messages, _ := history[0].Values["messages"].([]any)
	if len(messages) < 2 {
		t.Fatalf("values=%#v", history[0].Values)
	}
	first, _ := messages[0].(map[string]any)
	last, _ := messages[len(messages)-1].(map[string]any)
	if first["id"] != "user-1" || first["content"] != "hi" {
		t.Fatalf("first message=%#v", first)
	}
	if last["content"] != "hello from fake llm" {
		t.Fatalf("last message=%#v", last)
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
		"title":                       "Compat Thread",
		"mode":                        "thinking",
		"model_name":                  "deepseek/deepseek-r1",
		"reasoning_effort":            "high",
		"thinking_enabled":            false,
		"is_plan_mode":                true,
		"subagent_enabled":            true,
		"temperature":                 0.2,
		"max_tokens":                  321,
		"checkpoint_id":               "cp-1",
		"parent_checkpoint_id":        "cp-parent-1",
		"checkpoint_ns":               "ns-1",
		"parent_checkpoint_ns":        "ns-parent-1",
		"checkpoint_thread_id":        "checkpoint-thread-1",
		"parent_checkpoint_thread_id": "checkpoint-thread-parent-1",
		"next":                        []any{"lead_agent"},
		"tasks": []any{
			map[string]any{"id": "task-1", "name": "lead_agent"},
		},
		"interrupts": []any{
			map[string]any{"value": "Need input"},
		},
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
	configurable, _ := state.Config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" || configurable["model_name"] != "deepseek/deepseek-r1" || configurable["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v", state.Config)
	}
	if state.Checkpoint == nil || state.Checkpoint["checkpoint_id"] != "cp-1" || state.Checkpoint["checkpoint_ns"] != "ns-1" || state.Checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", state.Checkpoint)
	}
	if state.ParentCheckpoint == nil || state.ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || state.ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || state.ParentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", state.ParentCheckpoint)
	}
	if len(state.Next) != 1 || state.Next[0] != "lead_agent" {
		t.Fatalf("next=%#v", state.Next)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("tasks=%#v", state.Tasks)
	}
	if len(state.Interrupts) != 1 {
		t.Fatalf("interrupts=%#v", state.Interrupts)
	}
}

func TestThreadStatePreservesMessageCompatShape(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-message-shape"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{
		{
			ID:        "ai-1",
			SessionID: threadID,
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
				"reasoning_content":   "Need to inspect report",
				"usage_input_tokens":  "10",
				"usage_output_tokens": "5",
				"usage_total_tokens":  "15",
			},
		},
		{
			ID:        "tool-1",
			SessionID: threadID,
			Role:      models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "call-1",
				ToolName: "present_files",
				Status:   models.CallStatusCompleted,
				Content:  "presented",
				Duration: 1500 * time.Millisecond,
				Data: map[string]any{
					"files": []string{"/tmp/report.md"},
				},
			},
			Metadata: map[string]string{
				"message_status": "success",
			},
		},
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

	messages, _ := state.Values["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("values=%#v", state.Values)
	}
	ai, _ := messages[0].(map[string]any)
	if ai["id"] != "ai-1" {
		t.Fatalf("message=%#v", ai)
	}
	toolCalls, _ := ai["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("message=%#v", ai)
	}
	additionalKwargs, _ := ai["additional_kwargs"].(map[string]any)
	if additionalKwargs["reasoning_content"] != "Need to inspect report" {
		t.Fatalf("additional_kwargs=%#v", additionalKwargs)
	}
	usage, _ := ai["usage_metadata"].(map[string]any)
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) || usage["total_tokens"] != float64(15) {
		t.Fatalf("usage_metadata=%#v", usage)
	}
	tool, _ := messages[1].(map[string]any)
	if tool["id"] != "tool-1" || tool["tool_call_id"] != "call-1" || tool["status"] != "success" {
		t.Fatalf("message=%#v", tool)
	}
	data, _ := tool["data"].(map[string]any)
	if data["duration"] != "1.5s" {
		t.Fatalf("data=%#v", data)
	}
}

func TestThreadCreateAcceptsMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	resp, err := http.Post(
		ts.URL+"/threads",
		"application/json",
		strings.NewReader(`{"thread_id":"thread-create-messages","messages":[{"id":"m1","type":"human","content":"hello"}]}`),
	)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	values, _ := thread["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", values)
	}
	message, _ := messages[0].(map[string]any)
	if message["id"] != "m1" || message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}

	s.sessionsMu.RLock()
	session := s.sessions["thread-create-messages"]
	s.sessionsMu.RUnlock()
	if session == nil || len(session.Messages) != 1 || session.Messages[0].Content != "hello" {
		t.Fatalf("session=%#v", session)
	}
}

func TestThreadStatePatchAcceptsMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-write-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var state ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	messages, _ := state.Values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", state.Values)
	}
	message, _ := messages[0].(map[string]any)
	if message["id"] != "m1" || message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}

	s.sessionsMu.RLock()
	session := s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if session == nil || len(session.Messages) != 1 || session.Messages[0].Content != "hello" {
		t.Fatalf("session=%#v", session)
	}
}

func TestThreadStatePostAcceptsTopLevelMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-post-top-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"messages":[{"id":"m1","type":"human","content":"hello"}]}`),
	)
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

	var state ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	messages, _ := state.Values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", state.Values)
	}
	message, _ := messages[0].(map[string]any)
	if message["id"] != "m1" || message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}
}

func TestThreadStatePatchClearsMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-clear-messages"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{{
		ID:        "m1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var state ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	messages, _ := state.Values["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("values=%#v", state.Values)
	}

	s.sessionsMu.RLock()
	session = s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if session == nil || len(session.Messages) != 0 {
		t.Fatalf("session=%#v", session)
	}
}

func TestThreadUpdateClearsMessagesWithNull(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-update-clear-messages"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{{
		ID:        "m1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID,
		strings.NewReader(`{"messages":null}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	values, _ := thread["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("values=%#v", values)
	}

	s.sessionsMu.RLock()
	session = s.sessions[threadID]
	s.sessionsMu.RUnlock()
	if session == nil || len(session.Messages) != 0 {
		t.Fatalf("session=%#v", session)
	}
}

func TestThreadUpdateAcceptsTopLevelMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-update-top-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID,
		strings.NewReader(`{"messages":[{"id":"m1","type":"human","content":"hello"}]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	values, _ := thread["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", values)
	}
	message, _ := messages[0].(map[string]any)
	if message["id"] != "m1" || message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}
}

func TestThreadCreateMessageWritesPersistAcrossReload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	resp, err := http.Post(
		ts.URL+"/threads",
		"application/json",
		strings.NewReader(`{"thread_id":"thread-create-reload-messages","messages":[{"id":"m1","type":"human","content":"hello"}]}`),
	)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	loaded := &Server{
		dataRoot: s.dataRoot,
		sessions: map[string]*Session{},
	}
	loaded.loadPersistedThreads()

	session := loaded.sessions["thread-create-reload-messages"]
	if session == nil || len(session.Messages) != 1 {
		t.Fatalf("session=%#v", session)
	}
	if session.Messages[0].ID != "m1" || session.Messages[0].Content != "hello" {
		t.Fatalf("message=%#v", session.Messages[0])
	}
}

func TestThreadUpdateMessageWritesPersistAcrossReload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-update-reload-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID,
		strings.NewReader(`{"messages":[{"id":"m1","type":"human","content":"hello"}]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update thread: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	loaded := &Server{
		dataRoot: s.dataRoot,
		sessions: map[string]*Session{},
	}
	loaded.loadPersistedThreads()

	session := loaded.sessions[threadID]
	if session == nil || len(session.Messages) != 1 {
		t.Fatalf("session=%#v", session)
	}
	if session.Messages[0].ID != "m1" || session.Messages[0].Content != "hello" {
		t.Fatalf("message=%#v", session.Messages[0])
	}
}

func TestThreadHistoryReflectsWrittenMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-history-written-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	historyResp, err := http.Get(ts.URL + "/threads/" + threadID + "/history?limit=1")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer historyResp.Body.Close()
	if historyResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(historyResp.Body)
		t.Fatalf("status=%d body=%s", historyResp.StatusCode, string(b))
	}

	var history []ThreadState
	if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history len=%d", len(history))
	}
	messages, _ := history[0].Values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", history[0].Values)
	}
	message, _ := messages[0].(map[string]any)
	if message["id"] != "m1" || message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}
}

func TestThreadHistoryReflectsClearedMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-history-cleared-messages"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{{
		ID:        "m1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	historyResp, err := http.Get(ts.URL + "/threads/" + threadID + "/history?limit=1")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer historyResp.Body.Close()
	if historyResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(historyResp.Body)
		t.Fatalf("status=%d body=%s", historyResp.StatusCode, string(b))
	}

	var history []ThreadState
	if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history len=%d", len(history))
	}
	messages, _ := history[0].Values["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("values=%#v", history[0].Values)
	}
}

func TestThreadSearchReflectsWrittenMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-search-written-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	searchResp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["threadId","values"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer searchResp.Body.Close()
	if searchResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(searchResp.Body)
		t.Fatalf("status=%d body=%s", searchResp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(searchResp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == threadID {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	values, _ := selected["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", values)
	}
	message, _ := messages[0].(map[string]any)
	if message["id"] != "m1" || message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}
}

func TestThreadSearchReflectsClearedMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-search-cleared-messages"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{{
		ID:        "m1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	searchResp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"select":["threadId","values"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer searchResp.Body.Close()
	if searchResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(searchResp.Body)
		t.Fatalf("status=%d body=%s", searchResp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(searchResp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var selected map[string]any
	for _, thread := range threads {
		if thread["thread_id"] == threadID {
			selected = thread
			break
		}
	}
	if selected == nil {
		t.Fatalf("missing projected thread in %#v", threads)
	}
	values, _ := selected["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("values=%#v", values)
	}
}

func TestThreadGetReflectsWrittenMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-get-written-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	getResp, err := http.Get(ts.URL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(getResp.Body)
		t.Fatalf("status=%d body=%s", getResp.StatusCode, string(b))
	}

	var thread map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	values, _ := thread["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", values)
	}
	message, _ := messages[0].(map[string]any)
	if message["id"] != "m1" || message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}
}

func TestThreadGetReflectsClearedMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-get-cleared-messages"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{{
		ID:        "m1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	getResp, err := http.Get(ts.URL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(getResp.Body)
		t.Fatalf("status=%d body=%s", getResp.StatusCode, string(b))
	}

	var thread map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	values, _ := thread["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("values=%#v", values)
	}
}

func TestThreadStateGetReflectsWrittenMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-get-written-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	stateResp, err := http.Get(ts.URL + "/threads/" + threadID + "/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer stateResp.Body.Close()
	if stateResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(stateResp.Body)
		t.Fatalf("status=%d body=%s", stateResp.StatusCode, string(b))
	}

	var state ThreadState
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	messages, _ := state.Values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", state.Values)
	}
	message, _ := messages[0].(map[string]any)
	if message["id"] != "m1" || message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}
}

func TestThreadStateGetReflectsClearedMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-get-cleared-messages"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{{
		ID:        "m1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	stateResp, err := http.Get(ts.URL + "/threads/" + threadID + "/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer stateResp.Body.Close()
	if stateResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(stateResp.Body)
		t.Fatalf("status=%d body=%s", stateResp.StatusCode, string(b))
	}

	var state ThreadState
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	messages, _ := state.Values["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("values=%#v", state.Values)
	}
}

func TestThreadStateMessageWritesPersistAcrossReload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-reload-messages"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"messages":[{"id":"m1","type":"human","content":"hello"}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch state: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	loaded := &Server{
		dataRoot: s.dataRoot,
		sessions: map[string]*Session{},
	}
	loaded.loadPersistedThreads()

	session := loaded.sessions[threadID]
	if session == nil {
		t.Fatal("expected thread restored")
	}
	if len(session.Messages) != 1 {
		t.Fatalf("messages=%#v", session.Messages)
	}
	if session.Messages[0].ID != "m1" || session.Messages[0].Content != "hello" {
		t.Fatalf("message=%#v", session.Messages[0])
	}
}

func TestThreadGetIncludesCompatShape(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-get-shape"
	session := s.ensureSession(threadID, map[string]any{
		"agent_name":                  "writer",
		"agent_type":                  "deep_research",
		"assistant_id":                "assistant-1",
		"graph_id":                    "graph-1",
		"run_id":                      "run-1",
		"step":                        7,
		"title":                       "Compat Thread",
		"todos":                       []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox":                     map[string]any{"sandbox_id": "sb-1"},
		"viewed_images":               map[string]any{"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"}},
		"artifacts":                   []any{"/tmp/report.html"},
		"thread_data":                 map[string]any{"workspace_path": "/tmp/workspace", "uploads_path": "/tmp/uploads", "outputs_path": "/tmp/outputs"},
		"uploaded_files":              []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt", "status": "uploaded"}},
		"mode":                        "thinking",
		"model_name":                  "deepseek/deepseek-r1",
		"reasoning_effort":            "high",
		"thinking_enabled":            false,
		"is_plan_mode":                true,
		"subagent_enabled":            true,
		"temperature":                 0.2,
		"max_tokens":                  321,
		"checkpoint_id":               "cp-1",
		"parent_checkpoint_id":        "cp-parent-1",
		"checkpoint_ns":               "ns-1",
		"parent_checkpoint_ns":        "ns-parent-1",
		"checkpoint_thread_id":        "checkpoint-thread-1",
		"parent_checkpoint_thread_id": "checkpoint-thread-parent-1",
		"next":                        []any{"lead_agent"},
		"tasks":                       []any{map[string]any{"id": "task-1", "name": "lead_agent"}},
		"interrupts":                  []any{map[string]any{"value": "Need input"}},
	})
	session.Messages = []models.Message{{
		ID:        "m1",
		SessionID: threadID,
		Role:      models.RoleHuman,
		Content:   "hello",
	}}
	session.Status = "busy"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "brief.txt"), []byte("brief"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	resp, err := http.Get(ts.URL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	if thread["thread_id"] != threadID || thread["status"] != "busy" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["assistant_id"] != "assistant-1" || thread["graph_id"] != "graph-1" || thread["run_id"] != "run-1" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["agent_name"] != "writer" || thread["agent_type"] != "deep_research" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["title"] != "Compat Thread" {
		t.Fatalf("thread=%#v", thread)
	}
	artifacts, _ := thread["artifacts"].([]any)
	if len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("thread=%#v", thread)
	}
	todos, _ := thread["todos"].([]any)
	if len(todos) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	sandboxState, _ := thread["sandbox"].(map[string]any)
	if sandboxState["sandbox_id"] != "sb-1" {
		t.Fatalf("thread=%#v", thread)
	}
	threadData, _ := thread["thread_data"].(map[string]any)
	if threadData["uploads_path"] != s.uploadsDir(threadID) {
		t.Fatalf("thread=%#v", thread)
	}
	uploadedFiles, _ := thread["uploaded_files"].([]any)
	if len(uploadedFiles) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	uploadedFile, _ := uploadedFiles[0].(map[string]any)
	if uploadedFile["filename"] != "brief.txt" {
		t.Fatalf("thread=%#v", thread)
	}
	viewedImages, _ := thread["viewed_images"].(map[string]any)
	if len(viewedImages) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["mode"] != "thinking" || thread["model_name"] != "deepseek/deepseek-r1" || thread["reasoning_effort"] != "high" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["thinking_enabled"] != false || thread["is_plan_mode"] != true || thread["subagent_enabled"] != true {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["temperature"] != 0.2 {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["max_tokens"] != float64(321) && thread["max_tokens"] != int64(321) && thread["max_tokens"] != 321 {
		t.Fatalf("thread=%#v", thread)
	}
	values, _ := thread["values"].(map[string]any)
	if values["title"] != "Compat Thread" {
		t.Fatalf("values=%#v", values)
	}
	messages, _ := values["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("values=%#v", values)
	}
	firstMessage, _ := messages[0].(map[string]any)
	if firstMessage["content"] != "hello" {
		t.Fatalf("message=%#v", firstMessage)
	}
	config, _ := thread["config"].(map[string]any)
	configurable, _ := config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" || configurable["model_name"] != "deepseek/deepseek-r1" || configurable["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v", config)
	}
	metadata, _ := thread["metadata"].(map[string]any)
	if metadata["checkpoint_id"] != "cp-1" || metadata["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["assistant_id"] != "assistant-1" || metadata["graph_id"] != "graph-1" || metadata["run_id"] != "run-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if thread["checkpoint_id"] != "cp-1" || thread["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["checkpoint_ns"] != "ns-1" || thread["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["checkpoint_thread_id"] != "checkpoint-thread-1" || thread["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["step"] != float64(7) && thread["step"] != 7 {
		t.Fatalf("thread=%#v", thread)
	}
	next, _ := thread["next"].([]any)
	if len(next) != 1 || next[0] != "lead_agent" {
		t.Fatalf("thread=%#v", thread)
	}
	tasks, _ := thread["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	interrupts, _ := thread["interrupts"].([]any)
	if len(interrupts) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	checkpoint, _ := thread["checkpoint"].(map[string]any)
	if checkpoint["checkpoint_id"] != "cp-1" || checkpoint["checkpoint_ns"] != "ns-1" || checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", thread["checkpoint"])
	}
	parentCheckpoint, _ := thread["parent_checkpoint"].(map[string]any)
	if parentCheckpoint["checkpoint_id"] != "cp-parent-1" || parentCheckpoint["checkpoint_ns"] != "ns-parent-1" || parentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", thread["parent_checkpoint"])
	}
}

func TestThreadGetPreservesMessageCompatShape(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-get-message-shape"
	session := s.ensureSession(threadID, nil)
	session.Messages = []models.Message{
		{
			ID:        "ai-1",
			SessionID: threadID,
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
				"reasoning_content":   "Need to inspect report",
				"usage_input_tokens":  "10",
				"usage_output_tokens": "5",
				"usage_total_tokens":  "15",
			},
		},
		{
			ID:        "tool-1",
			SessionID: threadID,
			Role:      models.RoleTool,
			ToolResult: &models.ToolResult{
				CallID:   "call-1",
				ToolName: "present_files",
				Status:   models.CallStatusCompleted,
				Content:  "presented",
				Duration: 1500 * time.Millisecond,
				Data: map[string]any{
					"files": []string{"/tmp/report.md"},
				},
			},
			Metadata: map[string]string{
				"message_status": "success",
			},
		},
	}

	resp, err := http.Get(ts.URL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}

	values, _ := thread["values"].(map[string]any)
	messages, _ := values["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("values=%#v", values)
	}
	ai, _ := messages[0].(map[string]any)
	if ai["id"] != "ai-1" {
		t.Fatalf("message=%#v", ai)
	}
	toolCalls, _ := ai["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("message=%#v", ai)
	}
	additionalKwargs, _ := ai["additional_kwargs"].(map[string]any)
	if additionalKwargs["reasoning_content"] != "Need to inspect report" {
		t.Fatalf("additional_kwargs=%#v", additionalKwargs)
	}
	usage, _ := ai["usage_metadata"].(map[string]any)
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) || usage["total_tokens"] != float64(15) {
		t.Fatalf("usage_metadata=%#v", usage)
	}
	tool, _ := messages[1].(map[string]any)
	if tool["id"] != "tool-1" || tool["tool_call_id"] != "call-1" || tool["status"] != "success" {
		t.Fatalf("message=%#v", tool)
	}
	data, _ := tool["data"].(map[string]any)
	if data["duration"] != "1.5s" {
		t.Fatalf("data=%#v", data)
	}
}

func TestThreadSearchSupportsStepSort(t *testing.T) {
	s, ts := newCompatTestServer(t)
	first := s.ensureSession("thread-search-step-b", map[string]any{"step": 7})
	second := s.ensureSession("thread-search-step-a", map[string]any{"step": 3})
	first.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	second.UpdatedAt = time.Now().UTC()

	resp, err := http.Post(
		ts.URL+"/threads/search",
		"application/json",
		strings.NewReader(`{"limit":10,"sortBy":"step","sortOrder":"asc","select":["threadId","step"]}`),
	)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(threads) < 2 {
		t.Fatalf("threads=%#v", threads)
	}
	if threads[0]["thread_id"] != "thread-search-step-a" {
		t.Fatalf("first thread=%v want thread-search-step-a", threads[0]["thread_id"])
	}
	if threads[0]["step"] != float64(3) && threads[0]["step"] != 3 {
		t.Fatalf("step=%#v", threads[0]["step"])
	}
}

func TestThreadHistoryAcceptsSnakeCasePageSize(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-history-page-size"
	s.ensureSession(threadID, map[string]any{"title": "Current"})
	if err := s.appendThreadHistorySnapshot(threadID); err != nil {
		t.Fatalf("append history: %v", err)
	}
	s.sessionsMu.Lock()
	session := s.sessions[threadID]
	session.Metadata["title"] = "Latest"
	session.UpdatedAt = time.Now().UTC()
	s.sessionsMu.Unlock()
	if err := s.appendThreadHistorySnapshot(threadID); err != nil {
		t.Fatalf("append history: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/"+threadID+"/history", strings.NewReader(`{"page_size":1}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("history request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var history []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("len=%d history=%#v", len(history), history)
	}
	values, _ := history[0]["values"].(map[string]any)
	if values["title"] != "Latest" {
		t.Fatalf("history=%#v", history)
	}
}

func TestThreadHistoryAcceptsSnakeCasePageSizeQuery(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-history-page-size-query"
	s.ensureSession(threadID, map[string]any{"title": "Current"})
	if err := s.appendThreadHistorySnapshot(threadID); err != nil {
		t.Fatalf("append history: %v", err)
	}
	s.sessionsMu.Lock()
	session := s.sessions[threadID]
	session.Metadata["title"] = "Latest"
	session.UpdatedAt = time.Now().UTC()
	s.sessionsMu.Unlock()
	if err := s.appendThreadHistorySnapshot(threadID); err != nil {
		t.Fatalf("append history: %v", err)
	}

	resp, err := http.Get(ts.URL + "/threads/" + threadID + "/history?page_size=1")
	if err != nil {
		t.Fatalf("history request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var history []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("len=%d history=%#v", len(history), history)
	}
	values, _ := history[0]["values"].(map[string]any)
	if values["title"] != "Latest" {
		t.Fatalf("history=%#v", history)
	}
}

func TestThreadStatePostPersistsCompatFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-post"
	s.ensureSession(threadID, nil)

	body := `{"metadata":{"mode":"thinking","model_name":"deepseek/deepseek-r1","reasoning_effort":"high","thinking_enabled":false,"is_plan_mode":true,"subagent_enabled":true,"temperature":0.2,"max_tokens":321,"checkpoint_id":"cp-1","parent_checkpoint_id":"cp-parent-1","checkpoint_ns":"ns-1","parent_checkpoint_ns":"ns-parent-1","checkpoint_thread_id":"checkpoint-thread-1","parent_checkpoint_thread_id":"checkpoint-thread-parent-1","next":["lead_agent"],"tasks":[{"id":"task-1","name":"lead_agent"}],"interrupts":[{"value":"Need input"}]},"values":{"title":"Updated","todos":[{"content":"ship sqlite","status":"in_progress"}],"sandbox":{"sandbox_id":"sb-2"},"artifacts":["/tmp/report.html"],"viewed_images":{"/tmp/chart.png":{"base64":"xyz","mime_type":"image/png"}}}}`
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
	var state ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	configurable, _ := state.Config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" || configurable["model_name"] != "deepseek/deepseek-r1" || configurable["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v", state.Config)
	}
	if state.Checkpoint == nil || state.Checkpoint["checkpoint_id"] != "cp-1" || state.Checkpoint["checkpoint_ns"] != "ns-1" || state.Checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", state.Checkpoint)
	}
	if state.ParentCheckpoint == nil || state.ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || state.ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || state.ParentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", state.ParentCheckpoint)
	}
	if len(state.Next) != 1 || state.Next[0] != "lead_agent" {
		t.Fatalf("next=%#v", state.Next)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("tasks=%#v", state.Tasks)
	}
	if len(state.Interrupts) != 1 {
		t.Fatalf("interrupts=%#v", state.Interrupts)
	}
	artifacts := anyStringSlice(state.Values["artifacts"])
	if len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
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
	if artifacts := anyStringSlice(session.Metadata["artifacts"]); len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("artifacts=%#v", session.Metadata["artifacts"])
	}
}

func TestThreadStatePostAcceptsCamelCaseCompatValues(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-post-camel"
	s.ensureSession(threadID, nil)

	body := `{"values":{"threadData":{"workspace_path":"/tmp/workspace","uploads_path":"/tmp/uploads","outputs_path":"/tmp/outputs"},"uploadedFiles":[{"filename":"notes.txt","path":"/tmp/uploads/notes.txt","size":42,"status":"uploaded"}]}}`
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

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	threadData, _ := state.Values["thread_data"].(map[string]any)
	if threadData["workspace_path"] != "/tmp/workspace" || threadData["uploads_path"] != "/tmp/uploads" || threadData["outputs_path"] != "/tmp/outputs" {
		t.Fatalf("thread_data=%#v", state.Values["thread_data"])
	}
	switch uploadedFiles := state.Values["uploaded_files"].(type) {
	case []map[string]any:
		if len(uploadedFiles) != 1 || uploadedFiles[0]["filename"] != "notes.txt" || uploadedFiles[0]["path"] != "/tmp/uploads/notes.txt" {
			t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
		}
	case []any:
		if len(uploadedFiles) != 1 {
			t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
		}
		file, _ := uploadedFiles[0].(map[string]any)
		if file["filename"] != "notes.txt" || file["path"] != "/tmp/uploads/notes.txt" {
			t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
		}
	default:
		t.Fatalf("uploaded_files=%#v", state.Values["uploaded_files"])
	}
}

func TestThreadStatePostAcceptsMetadata(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-post-metadata"
	s.ensureSession(threadID, nil)

	body := `{"metadata":{"assistantId":"lead_agent","graphId":"lead_agent","modelName":"deepseek/deepseek-r1","reasoningEffort":"high","agentName":"planner","agentType":"research","thinkingEnabled":false,"isPlanMode":true,"subagentEnabled":true,"checkpointId":"cp-1","parentCheckpointId":"cp-parent-1","checkpointNs":"ns-1","parentCheckpointNs":"ns-parent-1","checkpointThreadId":"checkpoint-thread-1","parentCheckpointThreadId":"checkpoint-thread-parent-1","Temperature":0.2,"maxTokens":321},"values":{"title":"Updated"}}`
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

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	if state.Metadata["assistant_id"] != "lead_agent" || state.Metadata["graph_id"] != "lead_agent" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["model_name"] != "deepseek/deepseek-r1" || state.Metadata["reasoning_effort"] != "high" || state.Metadata["agent_name"] != "planner" || state.Metadata["agent_type"] != "research" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["checkpoint_id"] != "cp-1" || state.Metadata["parent_checkpoint_id"] != "cp-parent-1" || state.Metadata["checkpoint_ns"] != "ns-1" || state.Metadata["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["checkpoint_thread_id"] != "checkpoint-thread-1" || state.Metadata["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["thinking_enabled"] != false || state.Metadata["is_plan_mode"] != true || state.Metadata["subagent_enabled"] != true {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["temperature"] != 0.2 {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["max_tokens"] != float64(321) && state.Metadata["max_tokens"] != int64(321) && state.Metadata["max_tokens"] != 321 {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Values["title"] != "Updated" {
		t.Fatalf("title=%v", state.Values["title"])
	}
	configurable, _ := state.Config["configurable"].(map[string]any)
	if configurable["model_name"] != "deepseek/deepseek-r1" || configurable["reasoning_effort"] != "high" || configurable["agent_name"] != "planner" || configurable["agent_type"] != "research" {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", state.Config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v", state.Config)
	}
	if state.Checkpoint == nil || state.Checkpoint["checkpoint_id"] != "cp-1" || state.Checkpoint["checkpoint_ns"] != "ns-1" || state.Checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", state.Checkpoint)
	}
	if state.ParentCheckpoint == nil || state.ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || state.ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || state.ParentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", state.ParentCheckpoint)
	}
}

func TestThreadStatePostAcceptsCheckpointObjects(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-post-checkpoint-objects"
	s.ensureSession(threadID, nil)

	body := `{"metadata":{"checkpoint":{"checkpointId":"cp-1","checkpointNs":"ns-1","threadId":"checkpoint-thread-1"},"parentCheckpoint":{"checkpointId":"cp-parent-1","checkpointNs":"ns-parent-1","threadId":"checkpoint-thread-parent-1"}}}`
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

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	if state.Metadata["checkpoint_id"] != "cp-1" || state.Metadata["checkpoint_ns"] != "ns-1" || state.Metadata["checkpoint_thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Metadata["parent_checkpoint_id"] != "cp-parent-1" || state.Metadata["parent_checkpoint_ns"] != "ns-parent-1" || state.Metadata["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("metadata=%#v", state.Metadata)
	}
	if state.Checkpoint == nil || state.Checkpoint["checkpoint_id"] != "cp-1" || state.Checkpoint["checkpoint_ns"] != "ns-1" || state.Checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", state.Checkpoint)
	}
	if state.ParentCheckpoint == nil || state.ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || state.ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || state.ParentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", state.ParentCheckpoint)
	}
}

func TestThreadStatePostClearsValueSummaries(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-post-clear-values"
	s.ensureSession(threadID, map[string]any{
		"title":          "Before",
		"todos":          []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox":        map[string]any{"sandbox_id": "sb-1"},
		"artifacts":      []any{"/tmp/report.html"},
		"viewed_images":  map[string]any{"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"}},
		"thread_data":    map[string]any{"workspace_path": "/tmp/workspace"},
		"uploaded_files": []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt"}},
	})

	req, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"title":"","todos":[],"sandbox":{},"artifacts":[],"viewedImages":{},"threadData":{},"uploadedFiles":[]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var stateResp ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&stateResp); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if stateResp.Values["title"] != "" {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if len(anyStringSlice(stateResp.Values["artifacts"])) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if todos := anySlice(stateResp.Values["todos"]); len(todos) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if sandbox, _ := stateResp.Values["sandbox"].(map[string]any); len(sandbox) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if viewedImages, _ := stateResp.Values["viewed_images"].(map[string]any); len(viewedImages) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	threadData, _ := stateResp.Values["thread_data"].(map[string]any)
	if threadData["uploads_path"] != s.uploadsDir(threadID) {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if uploadedFiles := anySlice(stateResp.Values["uploaded_files"]); len(uploadedFiles) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	for _, key := range []string{"title", "todos", "sandbox", "artifacts", "viewed_images", "thread_data", "uploaded_files"} {
		if _, ok := state.Metadata[key]; ok {
			t.Fatalf("metadata=%#v", state.Metadata)
		}
	}
}

func TestThreadStatePatchClearsValueSummariesWithNulls(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-patch-clear-values-null"
	s.ensureSession(threadID, map[string]any{
		"title":          "Before",
		"todos":          []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox":        map[string]any{"sandbox_id": "sb-1"},
		"artifacts":      []any{"/tmp/report.html"},
		"viewed_images":  map[string]any{"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"}},
		"thread_data":    map[string]any{"workspace_path": "/tmp/workspace"},
		"uploaded_files": []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt"}},
	})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"title":null,"todos":null,"sandbox":null,"artifacts":null,"viewedImages":null,"threadData":null,"uploadedFiles":null}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	for _, key := range []string{"title", "todos", "sandbox", "artifacts", "viewed_images", "thread_data", "uploaded_files"} {
		if _, ok := state.Metadata[key]; ok {
			t.Fatalf("metadata=%#v", state.Metadata)
		}
	}
}

func TestThreadStatePatchAcceptsValuesPayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-patch-values"
	s.ensureSession(threadID, map[string]any{"title": "Before"})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"metadata":{"mode":"thinking","modelName":"deepseek/deepseek-r1","reasoningEffort":"high","thinkingEnabled":false,"isPlanMode":true,"subagentEnabled":true,"Temperature":0.2,"maxTokens":321,"checkpointId":"cp-1","parentCheckpointId":"cp-parent-1","checkpointNs":"ns-1","parentCheckpointNs":"ns-parent-1","checkpointThreadId":"checkpoint-thread-1","parentCheckpointThreadId":"checkpoint-thread-parent-1","next":["lead_agent"],"tasks":[{"id":"task-1","name":"lead_agent"}],"interrupts":[{"value":"Need input"}]},"values":{"title":"After","artifacts":["/tmp/report.html"]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var stateResp ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&stateResp); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	configurable, _ := stateResp.Config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" || configurable["model_name"] != "deepseek/deepseek-r1" || configurable["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", stateResp.Config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", stateResp.Config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", stateResp.Config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v", stateResp.Config)
	}
	if stateResp.Checkpoint == nil || stateResp.Checkpoint["checkpoint_id"] != "cp-1" || stateResp.Checkpoint["checkpoint_ns"] != "ns-1" || stateResp.Checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", stateResp.Checkpoint)
	}
	if stateResp.ParentCheckpoint == nil || stateResp.ParentCheckpoint["checkpoint_id"] != "cp-parent-1" || stateResp.ParentCheckpoint["checkpoint_ns"] != "ns-parent-1" || stateResp.ParentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", stateResp.ParentCheckpoint)
	}
	if len(stateResp.Next) != 1 || stateResp.Next[0] != "lead_agent" {
		t.Fatalf("next=%#v", stateResp.Next)
	}
	if len(stateResp.Tasks) != 1 {
		t.Fatalf("tasks=%#v", stateResp.Tasks)
	}
	if len(stateResp.Interrupts) != 1 {
		t.Fatalf("interrupts=%#v", stateResp.Interrupts)
	}
	artifacts := anyStringSlice(stateResp.Values["artifacts"])
	if len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("artifacts=%#v", stateResp.Values["artifacts"])
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	if got := state.Values["title"]; got != "After" {
		t.Fatalf("title=%v want After", got)
	}
	if artifacts := anyStringSlice(state.Values["artifacts"]); len(artifacts) != 1 || artifacts[0] != "/tmp/report.html" {
		t.Fatalf("artifacts=%#v", state.Values["artifacts"])
	}
}

func TestThreadStatePatchAcceptsTopLevelValues(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-patch-top-level"
	s.ensureSession(threadID, map[string]any{"title": "Before"})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"title":"After Top"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	if got := state.Values["title"]; got != "After Top" {
		t.Fatalf("title=%v want After Top", got)
	}
}

func TestThreadStatePatchClearsValueSummaries(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-patch-clear-values"
	s.ensureSession(threadID, map[string]any{
		"title":          "Before",
		"todos":          []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox":        map[string]any{"sandbox_id": "sb-1"},
		"artifacts":      []any{"/tmp/report.html"},
		"viewed_images":  map[string]any{"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"}},
		"thread_data":    map[string]any{"workspace_path": "/tmp/workspace"},
		"uploaded_files": []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt"}},
	})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"title":"","todos":[],"sandbox":{},"artifacts":[],"viewedImages":{},"threadData":{},"uploadedFiles":[]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var stateResp ThreadState
	if err := json.NewDecoder(resp.Body).Decode(&stateResp); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if stateResp.Values["title"] != "" {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if len(anyStringSlice(stateResp.Values["artifacts"])) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if todos := anySlice(stateResp.Values["todos"]); len(todos) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if sandbox, _ := stateResp.Values["sandbox"].(map[string]any); len(sandbox) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if viewedImages, _ := stateResp.Values["viewed_images"].(map[string]any); len(viewedImages) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	threadData, _ := stateResp.Values["thread_data"].(map[string]any)
	if threadData["uploads_path"] != s.uploadsDir(threadID) {
		t.Fatalf("values=%#v", stateResp.Values)
	}
	if uploadedFiles := anySlice(stateResp.Values["uploaded_files"]); len(uploadedFiles) != 0 {
		t.Fatalf("values=%#v", stateResp.Values)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	for _, key := range []string{"title", "todos", "sandbox", "artifacts", "viewed_images", "thread_data", "uploaded_files"} {
		if _, ok := state.Metadata[key]; ok {
			t.Fatalf("metadata=%#v", state.Metadata)
		}
	}
}

func TestThreadHistoryReflectsClearedValueSummaries(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-history-clear-values"
	s.ensureSession(threadID, map[string]any{
		"title":          "Before",
		"todos":          []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox":        map[string]any{"sandbox_id": "sb-1"},
		"artifacts":      []any{"/tmp/report.html"},
		"viewed_images":  map[string]any{"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"}},
		"thread_data":    map[string]any{"workspace_path": "/tmp/workspace"},
		"uploaded_files": []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt"}},
	})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"title":"","todos":[],"sandbox":{},"artifacts":[],"viewedImages":{},"threadData":{},"uploadedFiles":[]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch request: %v", err)
	}
	resp.Body.Close()

	historyResp, err := http.Get(ts.URL + "/threads/" + threadID + "/history?limit=1")
	if err != nil {
		t.Fatalf("history request: %v", err)
	}
	defer historyResp.Body.Close()
	if historyResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(historyResp.Body)
		t.Fatalf("status=%d body=%s", historyResp.StatusCode, string(b))
	}

	var history []map[string]any
	if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	values, _ := history[0]["values"].(map[string]any)
	if values["title"] != "" {
		t.Fatalf("values=%#v", values)
	}
	if len(anyStringSlice(values["artifacts"])) != 0 {
		t.Fatalf("values=%#v", values)
	}
	if todos := anySlice(values["todos"]); len(todos) != 0 {
		t.Fatalf("values=%#v", values)
	}
	if sandbox, _ := values["sandbox"].(map[string]any); len(sandbox) != 0 {
		t.Fatalf("values=%#v", values)
	}
	if viewedImages, _ := values["viewed_images"].(map[string]any); len(viewedImages) != 0 {
		t.Fatalf("values=%#v", values)
	}
	threadData, _ := values["thread_data"].(map[string]any)
	if threadData["uploads_path"] != s.uploadsDir(threadID) {
		t.Fatalf("values=%#v", values)
	}
	if uploadedFiles := anySlice(values["uploaded_files"]); len(uploadedFiles) != 0 {
		t.Fatalf("values=%#v", values)
	}
	metadata, _ := history[0]["metadata"].(map[string]any)
	for _, key := range []string{"title", "todos", "sandbox", "artifacts", "viewed_images", "thread_data", "uploaded_files"} {
		if _, ok := metadata[key]; ok {
			t.Fatalf("metadata=%#v", metadata)
		}
	}
}

func TestThreadUpdateAcceptsValuesPayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-update-values"
	s.ensureSession(threadID, map[string]any{"title": "Before", "mode": "flash"})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID,
		strings.NewReader(`{"metadata":{"assistantId":"assistant-1","graphId":"graph-1","runId":"run-1","mode":"thinking","modelName":"deepseek/deepseek-r1","reasoningEffort":"high","agentName":"planner","agentType":"research","thinkingEnabled":false,"isPlanMode":true,"subagentEnabled":true,"Temperature":0.2,"maxTokens":321,"checkpointId":"cp-1","parentCheckpointId":"cp-parent-1","checkpointNs":"ns-1","parentCheckpointNs":"ns-parent-1","checkpointThreadId":"checkpoint-thread-1","parentCheckpointThreadId":"checkpoint-thread-parent-1"},"values":{"title":"After","todos":[{"content":"ship sqlite","status":"completed"}]}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}
	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	if thread["title"] != "After" {
		t.Fatalf("thread=%#v", thread)
	}
	todosSummary, _ := thread["todos"].([]any)
	if len(todosSummary) != 1 {
		t.Fatalf("thread=%#v", thread)
	}
	todoSummary, _ := todosSummary[0].(map[string]any)
	if todoSummary["content"] != "ship sqlite" {
		t.Fatalf("thread=%#v", thread)
	}
	values, _ := thread["values"].(map[string]any)
	if got := values["title"]; got != "After" {
		t.Fatalf("title=%v want After", got)
	}
	todosValues, _ := values["todos"].([]any)
	if len(todosValues) != 1 {
		t.Fatalf("values=%#v", values)
	}
	config, _ := thread["config"].(map[string]any)
	configurable, _ := config["configurable"].(map[string]any)
	if configurable["mode"] != "thinking" || configurable["model_name"] != "deepseek/deepseek-r1" || configurable["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", config)
	}
	if configurable["agent_name"] != "planner" || configurable["agent_type"] != "research" {
		t.Fatalf("config=%#v", config)
	}
	if configurable["thinking_enabled"] != false || configurable["is_plan_mode"] != true || configurable["subagent_enabled"] != true {
		t.Fatalf("config=%#v", config)
	}
	if configurable["temperature"] != 0.2 {
		t.Fatalf("config=%#v", config)
	}
	if configurable["max_tokens"] != float64(321) && configurable["max_tokens"] != int64(321) && configurable["max_tokens"] != 321 {
		t.Fatalf("config=%#v", config)
	}
	metadata, _ := thread["metadata"].(map[string]any)
	if metadata["checkpoint_id"] != "cp-1" || metadata["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["checkpoint_ns"] != "ns-1" || metadata["parent_checkpoint_ns"] != "ns-parent-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["checkpoint_thread_id"] != "checkpoint-thread-1" || metadata["parent_checkpoint_thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["assistant_id"] != "assistant-1" || metadata["graph_id"] != "graph-1" || metadata["run_id"] != "run-1" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["agent_name"] != "planner" || metadata["agent_type"] != "research" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if thread["assistant_id"] != "assistant-1" || thread["graph_id"] != "graph-1" || thread["run_id"] != "run-1" {
		t.Fatalf("thread=%#v", thread)
	}
	if thread["checkpoint_id"] != "cp-1" || thread["parent_checkpoint_id"] != "cp-parent-1" {
		t.Fatalf("thread=%#v", thread)
	}
	checkpoint, _ := thread["checkpoint"].(map[string]any)
	if checkpoint["checkpoint_id"] != "cp-1" || checkpoint["checkpoint_ns"] != "ns-1" || checkpoint["thread_id"] != "checkpoint-thread-1" {
		t.Fatalf("checkpoint=%#v", thread["checkpoint"])
	}
	parentCheckpoint, _ := thread["parent_checkpoint"].(map[string]any)
	if parentCheckpoint["checkpoint_id"] != "cp-parent-1" || parentCheckpoint["checkpoint_ns"] != "ns-parent-1" || parentCheckpoint["thread_id"] != "checkpoint-thread-parent-1" {
		t.Fatalf("parent_checkpoint=%#v", thread["parent_checkpoint"])
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	if got := state.Values["title"]; got != "After" {
		t.Fatalf("title=%v want After", got)
	}
	todos, ok := state.Values["todos"].([]map[string]any)
	if !ok || len(todos) != 1 {
		t.Fatalf("todos=%#v", state.Values["todos"])
	}
}

func TestThreadUpdateClearsValueSummaries(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-update-clear-values"
	s.ensureSession(threadID, map[string]any{
		"title":          "Before",
		"todos":          []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox":        map[string]any{"sandbox_id": "sb-1"},
		"artifacts":      []any{"/tmp/report.html"},
		"viewed_images":  map[string]any{"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"}},
		"thread_data":    map[string]any{"workspace_path": "/tmp/workspace"},
		"uploaded_files": []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt"}},
	})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID,
		strings.NewReader(`{"title":"","todos":[],"sandbox":{},"artifacts":[],"viewedImages":{},"threadData":{},"uploadedFiles":[]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var thread map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	if thread["title"] != "" {
		t.Fatalf("thread=%#v", thread)
	}
	if len(anyStringSlice(thread["artifacts"])) != 0 {
		t.Fatalf("thread=%#v", thread)
	}
	if todos := anySlice(thread["todos"]); len(todos) != 0 {
		t.Fatalf("thread=%#v", thread)
	}
	if sandbox, _ := thread["sandbox"].(map[string]any); len(sandbox) != 0 {
		t.Fatalf("thread=%#v", thread)
	}
	if viewedImages, _ := thread["viewed_images"].(map[string]any); len(viewedImages) != 0 {
		t.Fatalf("thread=%#v", thread)
	}
	threadData, _ := thread["thread_data"].(map[string]any)
	if threadData["uploads_path"] != s.uploadsDir(threadID) {
		t.Fatalf("thread=%#v", thread)
	}
	if uploadedFiles := anySlice(thread["uploaded_files"]); len(uploadedFiles) != 0 {
		t.Fatalf("thread=%#v", thread)
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	for _, key := range []string{"title", "todos", "sandbox", "artifacts", "viewed_images", "thread_data", "uploaded_files"} {
		if _, ok := state.Metadata[key]; ok {
			t.Fatalf("metadata=%#v", state.Metadata)
		}
	}
}

func TestThreadUpdateClearsValueSummariesWithNulls(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-update-clear-values-null"
	s.ensureSession(threadID, map[string]any{
		"title":          "Before",
		"todos":          []any{map[string]any{"content": "ship sqlite", "status": "pending"}},
		"sandbox":        map[string]any{"sandbox_id": "sb-1"},
		"artifacts":      []any{"/tmp/report.html"},
		"viewed_images":  map[string]any{"/tmp/chart.png": map[string]any{"base64": "xyz", "mime_type": "image/png"}},
		"thread_data":    map[string]any{"workspace_path": "/tmp/workspace"},
		"uploaded_files": []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt"}},
	})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID,
		strings.NewReader(`{"title":null,"todos":null,"sandbox":null,"artifacts":null,"viewedImages":null,"threadData":null,"uploadedFiles":null}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	for _, key := range []string{"title", "todos", "sandbox", "artifacts", "viewed_images", "thread_data", "uploaded_files"} {
		if _, ok := state.Metadata[key]; ok {
			t.Fatalf("metadata=%#v", state.Metadata)
		}
	}
}

func TestThreadStatePatchAcceptsCamelCaseViewedImages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-patch-viewed-images"
	s.ensureSession(threadID, nil)

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"viewedImages":{"/tmp/chart.png":{"base64":"xyz","mime_type":"image/png"}}}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	state := s.getThreadState(threadID)
	if state == nil {
		t.Fatal("state missing")
	}
	viewedImages, ok := state.Values["viewed_images"].(map[string]any)
	if !ok || len(viewedImages) != 1 {
		t.Fatalf("viewed_images=%#v", state.Values["viewed_images"])
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
	if metadata["thread_id"] != "thread-config-run" {
		t.Fatalf("thread_id=%v", metadata["thread_id"])
	}
	if metadata["assistant_id"] != "lead_agent" || metadata["graph_id"] != "lead_agent" {
		t.Fatalf("assistant/graph metadata=%#v", metadata)
	}
	if metadata["run_id"] == "" {
		t.Fatalf("run_id missing in metadata=%#v", metadata)
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

func TestThreadRunPersistsAgentNameFromContext(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]},"context":{"agent_name":"writer"}}`
	resp, err := http.Post(ts.URL+"/threads/thread-agent-name/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	threadResp, err := http.Get(ts.URL + "/threads/thread-agent-name")
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
	if metadata["agent_name"] != "writer" {
		t.Fatalf("agent_name=%v", metadata["agent_name"])
	}

	stateResp, err := http.Get(ts.URL + "/threads/thread-agent-name/state")
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
	if config["agent_name"] != "writer" {
		t.Fatalf("agent_name=%v", config["agent_name"])
	}
}

func TestThreadRunAcceptsCamelCaseConfigFields(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]},"config":{"configurable":{"modelName":"deepseek/deepseek-r1","thinkingEnabled":false,"isPlanMode":true,"subagentEnabled":true,"reasoningEffort":"high","agentType":"deep_research","temperature":0.2,"maxTokens":321}}}`
	resp, err := http.Post(ts.URL+"/threads/thread-camel-config/runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	threadResp, err := http.Get(ts.URL + "/threads/thread-camel-config")
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
	if metadata["model_name"] != "deepseek/deepseek-r1" || metadata["reasoning_effort"] != "high" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["thinking_enabled"] != false || metadata["is_plan_mode"] != true || metadata["subagent_enabled"] != true {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["temperature"] != 0.2 {
		t.Fatalf("metadata=%#v", metadata)
	}
	if metadata["max_tokens"] != float64(321) && metadata["max_tokens"] != 321 {
		t.Fatalf("metadata=%#v", metadata)
	}

	stateResp, err := http.Get(ts.URL + "/threads/thread-camel-config/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer stateResp.Body.Close()
	var state ThreadState
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	config, _ := state.Config["configurable"].(map[string]any)
	if config["model_name"] != "deepseek/deepseek-r1" || config["reasoning_effort"] != "high" {
		t.Fatalf("config=%#v", config)
	}
	if config["thinking_enabled"] != false || config["is_plan_mode"] != true || config["subagent_enabled"] != true {
		t.Fatalf("config=%#v", config)
	}
	if config["temperature"] != 0.2 {
		t.Fatalf("config=%#v", config)
	}
	if config["max_tokens"] != float64(321) && config["max_tokens"] != 321 && config["max_tokens"] != int64(321) {
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

func TestConvertToMessagesPreservesToolShape(t *testing.T) {
	s, _ := newCompatTestServer(t)
	input := []any{
		map[string]any{
			"id":      "ai-1",
			"type":    "ai",
			"content": "",
			"additional_kwargs": map[string]any{
				"reasoning_content": "Need to present files",
				"stop_reason":       "tool_calls",
			},
			"response_metadata": map[string]any{
				"model_name":         "deepseek-v3-2-251201",
				"model_provider":     "deepseek",
				"finish_reason":      "tool_calls",
				"system_fingerprint": "fpv0_abc",
			},
			"usage_metadata": map[string]any{
				"input_tokens":  10,
				"output_tokens": 5,
				"total_tokens":  15,
			},
			"tool_calls": []any{
				map[string]any{
					"id":   "call-1",
					"name": "present_files",
					"args": map[string]any{"filepaths": []any{"/tmp/report.md"}},
				},
			},
		},
		map[string]any{
			"id":           "tool-1",
			"type":         "tool",
			"name":         "present_files",
			"status":       "success",
			"tool_call_id": "call-1",
			"content":      "presented",
			"data": map[string]any{
				"status":   "completed",
				"duration": "1.5s",
				"data":     map[string]any{"filepaths": []any{"/tmp/report.md"}},
			},
		},
	}

	messages := s.convertToMessages("thread-tools", input)
	if len(messages) != 2 {
		t.Fatalf("messages=%d", len(messages))
	}
	if len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].ID != "call-1" || messages[0].ToolCalls[0].Name != "present_files" {
		t.Fatalf("tool_calls=%#v", messages[0].ToolCalls)
	}
	if messages[0].Metadata["reasoning_content"] != "Need to present files" || messages[0].Metadata["stop_reason"] != "tool_calls" {
		t.Fatalf("metadata=%#v", messages[0].Metadata)
	}
	if messages[0].Metadata["model_name"] != "deepseek-v3-2-251201" || messages[0].Metadata["model_provider"] != "deepseek" || messages[0].Metadata["system_fingerprint"] != "fpv0_abc" {
		t.Fatalf("metadata=%#v", messages[0].Metadata)
	}
	if messages[0].Metadata["usage_input_tokens"] != "10" || messages[0].Metadata["usage_output_tokens"] != "5" || messages[0].Metadata["usage_total_tokens"] != "15" {
		t.Fatalf("metadata=%#v", messages[0].Metadata)
	}
	if messages[1].ToolResult == nil {
		t.Fatalf("tool_result=nil message=%#v", messages[1])
	}
	if messages[1].ToolResult.CallID != "call-1" || messages[1].ToolResult.ToolName != "present_files" {
		t.Fatalf("tool_result=%#v", messages[1].ToolResult)
	}
	if messages[1].Metadata["message_status"] != "success" {
		t.Fatalf("metadata=%#v", messages[1].Metadata)
	}
	if messages[1].ToolResult.Duration != 1500*time.Millisecond {
		t.Fatalf("duration=%s", messages[1].ToolResult.Duration)
	}
	if got := anyStringSlice(messages[1].ToolResult.Data["filepaths"]); len(got) != 1 || got[0] != "/tmp/report.md" {
		t.Fatalf("tool_result data=%#v", messages[1].ToolResult.Data)
	}
	roundTrip := s.messagesToLangChain(messages)
	if usage := roundTrip[0].UsageMetadata; usage["input_tokens"] != 10 || usage["output_tokens"] != 5 || usage["total_tokens"] != 15 {
		t.Fatalf("usage_metadata=%#v", usage)
	}
	if roundTrip[1].Status != "success" {
		t.Fatalf("status=%q", roundTrip[1].Status)
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
				Duration: 1500 * time.Millisecond,
				Data: map[string]any{
					"files": []string{"/tmp/report.md"},
				},
			},
			Metadata: map[string]string{
				"message_status": "success",
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
	if converted[1].Status != "success" {
		t.Fatalf("status=%q", converted[1].Status)
	}
	if _, ok := converted[1].AdditionalKwargs["message_status"]; ok {
		t.Fatalf("additional_kwargs=%#v", converted[1].AdditionalKwargs)
	}
	if converted[1].Data["duration"] != "1.5s" {
		t.Fatalf("duration=%#v", converted[1].Data["duration"])
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

func TestArtifactGetAllowsRecoveredMessageHistoryPaths(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-artifact-history-read"
	session := s.ensureSession(threadID, map[string]any{"title": "Artifacts"})
	session.PresentFiles.Clear()
	target := filepath.Join(s.dataRoot, "report.md")
	if err := os.WriteFile(target, []byte("artifact from history"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	session.Messages = []models.Message{
		{
			ID:        "ai-1",
			SessionID: threadID,
			Role:      models.RoleAI,
			ToolCalls: []models.ToolCall{
				{
					ID:        "call-1",
					Name:      "present_file",
					Arguments: map[string]any{"path": target},
					Status:    models.CallStatusCompleted,
				},
			},
		},
	}

	resp, err := http.Get(ts.URL + "/api/threads/" + threadID + "/artifacts" + target)
	if err != nil {
		t.Fatalf("get artifact: %v", err)
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
	if string(body) != "artifact from history" {
		t.Fatalf("body=%q", string(body))
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
	startBlock := sseEventBlock(t, text, "tool_call_start")
	endBlock := sseEventBlock(t, text, "tool_call_end")
	aliasBlock := sseEventBlock(t, text, "on_tool_end")
	updatesBlock := sseEventBlock(t, text, "updates")
	if !strings.Contains(text, "event: tool_call_start") {
		t.Fatalf("missing tool_call_start event: %s", text)
	}
	if !strings.Contains(text, "event: tool_call_end") {
		t.Fatalf("missing tool_call_end event: %s", text)
	}
	if !strings.Contains(text, `"id":"call-1"`) {
		t.Fatalf("missing tool call id in tool events: %s", text)
	}
	if !strings.Contains(text, "event: on_tool_end") {
		t.Fatalf("missing on_tool_end event: %s", text)
	}
	if !strings.Contains(startBlock, `"name":"read_file"`) || !strings.Contains(endBlock, `"name":"read_file"`) {
		t.Fatalf("missing tool call name in start/end payloads: %s %s", startBlock, endBlock)
	}
	if !strings.Contains(aliasBlock, `"name":"read_file"`) || !strings.Contains(aliasBlock, `"data":{"id":"call-1"`) {
		t.Fatalf("missing on_tool_end payload shape: %s", aliasBlock)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`} {
		if strings.Contains(startBlock, forbidden) {
			t.Fatalf("unexpected tool_call_start field %s: %s", forbidden, startBlock)
		}
		if strings.Contains(endBlock, forbidden) {
			t.Fatalf("unexpected tool_call_end field %s: %s", forbidden, endBlock)
		}
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`} {
		if strings.Contains(aliasBlock, forbidden) {
			t.Fatalf("unexpected on_tool_end field %s: %s", forbidden, aliasBlock)
		}
	}
	if !strings.Contains(text, "event: updates") {
		t.Fatalf("missing updates event: %s", text)
	}
	if !strings.Contains(text, `"artifacts":[]`) {
		t.Fatalf("missing agent artifacts payload in updates event: %s", text)
	}
	if !strings.Contains(text, `"messages":[`) {
		t.Fatalf("missing agent messages in updates payload: %s", text)
	}
	if !strings.Contains(text, `"title":""`) {
		t.Fatalf("missing agent title in updates payload: %s", text)
	}
	if strings.Contains(text, `"values":`) {
		t.Fatalf("unexpected nested values payload in updates event: %s", text)
	}
	for _, forbidden := range []string{`"todos":`, `"sandbox":`, `"thread_data":`, `"uploaded_files":`, `"viewed_images":`, `"run_id":`, `"thread_id":`, `"assistant_id":`, `"metadata":`, `"config":`} {
		if strings.Contains(updatesBlock, forbidden) {
			t.Fatalf("unexpected extra updates payload field %s: %s", forbidden, updatesBlock)
		}
	}
	if !strings.Contains(text, "\"usage_metadata\":{\"input_tokens\":10,\"output_tokens\":5,\"total_tokens\":15}") {
		t.Fatalf("missing usage_metadata in messages-tuple: %s", text)
	}
	if !strings.Contains(text, "\"type\":\"tool\",\"id\":\"tool:call-1\"") {
		t.Fatalf("missing stable tool message id: %s", text)
	}
}

func TestThreadRunStreamEmitsToolCallEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = &fakeToolLLMProvider{}
	if err := os.WriteFile("/tmp/demo.txt", []byte("demo"), 0o644); err != nil {
		t.Fatalf("write tool fixture: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-tool-call/runs/stream", strings.NewReader(`{"assistant_id":"lead_agent","stream_mode":["events"],"input":{"messages":[{"role":"user","content":"inspect file"}]}}`))
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
	callBlock := sseEventBlock(t, text, "tool_call")
	if !strings.Contains(text, "event: tool_call") {
		t.Fatalf("missing tool_call event: %s", text)
	}
	if !strings.Contains(text, `"id":"call-1"`) || !strings.Contains(text, `"name":"read_file"`) {
		t.Fatalf("missing tool_call payload: %s", text)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`, `"data":{`} {
		if strings.Contains(callBlock, forbidden) {
			t.Fatalf("unexpected tool_call field %s: %s", forbidden, callBlock)
		}
	}
}

func TestThreadRunStreamEmitsChunkEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-chunk/runs/stream", strings.NewReader(`{"assistant_id":"lead_agent","stream_mode":["events"],"input":{"messages":[{"role":"user","content":"hello"}]}}`))
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
	chunkBlock := sseEventBlock(t, text, "chunk")
	if !strings.Contains(text, "event: chunk") {
		t.Fatalf("missing chunk event: %s", text)
	}
	if !strings.Contains(text, `"delta":"hello from fake llm"`) {
		t.Fatalf("missing chunk payload: %s", text)
	}
	if !strings.Contains(text, `"content":"hello from fake llm"`) {
		t.Fatalf("missing chunk content payload: %s", text)
	}
	if !strings.Contains(chunkBlock, `"role":"assistant"`) || !strings.Contains(chunkBlock, `"type":"ai"`) {
		t.Fatalf("missing chunk identity payload: %s", chunkBlock)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"status":`, `"data":{`} {
		if strings.Contains(chunkBlock, forbidden) {
			t.Fatalf("unexpected chunk field %s: %s", forbidden, chunkBlock)
		}
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
	valuesBlock := sseEventBlock(t, text, "values")
	if !strings.Contains(text, "event: values") {
		t.Fatalf("missing values event: %s", text)
	}
	if !strings.Contains(text, `"messages":[`) {
		t.Fatalf("missing messages in values payload: %s", text)
	}
	if !strings.Contains(text, `"content":"hello from fake llm"`) {
		t.Fatalf("missing final message in values payload: %s", text)
	}
	for _, forbidden := range []string{`"metadata":`, `"config":`, `"next":`, `"tasks":`, `"interrupts":`, `"checkpoint":`, `"parent_checkpoint":`} {
		if strings.Contains(valuesBlock, forbidden) {
			t.Fatalf("unexpected extra values payload field %s: %s", forbidden, valuesBlock)
		}
	}
	if strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("unexpected messages-tuple event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"`) {
		t.Fatalf("missing run_id in end payload: %s", text)
	}
	if strings.Contains(text, `"thread_id":"thread-stream-top-level"`) || strings.Contains(text, `"assistant_id":"lead_agent"`) {
		t.Fatalf("unexpected extra fields in end payload: %s", text)
	}
}

func TestThreadRunStreamModeUpdatesFiltersOtherEvents(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = &fakeToolLLMProvider{}
	if err := os.WriteFile("/tmp/demo.txt", []byte("demo"), 0o644); err != nil {
		t.Fatalf("write tool fixture: %v", err)
	}

	reqBody := `{"assistant_id":"lead_agent","stream_mode":["updates"],"input":{"messages":[{"role":"user","content":"inspect file"}]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-updates/runs/stream", strings.NewReader(reqBody))
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
	updatesBlock := sseEventBlock(t, text, "updates")
	if !strings.Contains(text, "event: updates") {
		t.Fatalf("missing updates event: %s", text)
	}
	if !strings.Contains(text, `"agent":{"artifacts":[`) {
		t.Fatalf("missing updates payload: %s", text)
	}
	if strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("unexpected messages-tuple event: %s", text)
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
	for _, forbidden := range []string{`"todos":`, `"sandbox":`, `"thread_data":`, `"uploaded_files":`, `"viewed_images":`, `"run_id":`, `"thread_id":`, `"assistant_id":`, `"metadata":`, `"config":`} {
		if strings.Contains(updatesBlock, forbidden) {
			t.Fatalf("unexpected extra updates payload field %s: %s", forbidden, updatesBlock)
		}
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
	if strings.Contains(text, `"thread_id":"thread-stream-values"`) || strings.Contains(text, `"assistant_id":"lead_agent"`) {
		t.Fatalf("unexpected extra fields in end payload: %s", text)
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
	if !strings.Contains(text, `"content":"hello from fake llm"`) {
		t.Fatalf("missing message payload: %s", text)
	}
	tupleBlocks := sseEventBlocks(text, "messages-tuple")
	if len(tupleBlocks) == 0 {
		t.Fatalf("missing tuple blocks: %s", text)
	}
	for _, block := range tupleBlocks {
		for _, forbidden := range []string{`"metadata":`, `"config":`, `"next":`, `"tasks":`, `"interrupts":`, `"checkpoint":`, `"parent_checkpoint":`, `"thread_id":`, `"assistant_id":`, `"run_id":`} {
			if strings.Contains(block, forbidden) {
				t.Fatalf("unexpected live tuple field %s: %s", forbidden, block)
			}
		}
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"`) {
		t.Fatalf("missing run_id in end payload: %s", text)
	}
	if strings.Contains(text, `"thread_id":"thread-stream-messages"`) || strings.Contains(text, `"assistant_id":"lead_agent"`) {
		t.Fatalf("unexpected extra fields in end payload: %s", text)
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
	if !strings.Contains(text, `"content":"hello from fake llm"`) {
		t.Fatalf("missing message payload: %s", text)
	}
	tupleBlocks := sseEventBlocks(text, "messages-tuple")
	if len(tupleBlocks) == 0 {
		t.Fatalf("missing tuple blocks: %s", text)
	}
	for _, block := range tupleBlocks {
		for _, forbidden := range []string{`"metadata":`, `"config":`, `"next":`, `"tasks":`, `"interrupts":`, `"checkpoint":`, `"parent_checkpoint":`, `"thread_id":`, `"assistant_id":`, `"run_id":`} {
			if strings.Contains(block, forbidden) {
				t.Fatalf("unexpected live tuple field %s: %s", forbidden, block)
			}
		}
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
}

func TestThreadRunStreamAcceptsTopLevelMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	reqBody := `{"assistant_id":"lead_agent","messages":[{"role":"user","content":"hello from top level"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-top-level/runs/stream", strings.NewReader(reqBody))
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
	if !strings.Contains(text, "event: metadata") {
		t.Fatalf("missing metadata event: %s", text)
	}
	if !strings.Contains(text, `"thread_id":"thread-stream-top-level"`) {
		t.Fatalf("missing thread_id in metadata: %s", text)
	}
	if !strings.Contains(text, `"assistant_id":"lead_agent"`) {
		t.Fatalf("missing assistant_id in metadata: %s", text)
	}
	if !strings.Contains(text, `"run_id":"`) {
		t.Fatalf("missing run_id in metadata: %s", text)
	}
	if strings.Contains(text, `"status":`) {
		t.Fatalf("unexpected status in metadata payload: %s", text)
	}
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, `"content":"hello from fake llm"`) {
		t.Fatalf("missing message payload: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"`) {
		t.Fatalf("missing run_id in end payload: %s", text)
	}
}

func TestThreadRunStreamEmitsErrorEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeErrorLLMProvider{}

	reqBody := `{"assistant_id":"lead_agent","stream_mode":["events"],"input":{"messages":[{"role":"user","content":"hello"}]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-error/runs/stream", strings.NewReader(reqBody))
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
	errorBlock := sseEventBlock(t, text, "error")
	if !strings.Contains(text, "event: error") {
		t.Fatalf("missing error event: %s", text)
	}
	if !strings.Contains(text, `"message":"boom"`) {
		t.Fatalf("missing error message: %s", text)
	}
	if !strings.Contains(text, `"error":"RunError"`) || !strings.Contains(text, `"name":"RunError"`) {
		t.Fatalf("missing error identity: %s", text)
	}
	if !strings.Contains(text, `"suggestion":"Retry the run or inspect the previous tool and model events."`) {
		t.Fatalf("missing error suggestion: %s", text)
	}
	if !strings.Contains(text, `"retryable":true`) {
		t.Fatalf("missing retryable flag: %s", text)
	}
	for _, forbidden := range []string{`"thread_id":`, `"run_id":`, `"assistant_id":`, `"messages":`, `"usage_metadata":`, `"tool_calls":`, `"tool_call_id":`} {
		if strings.Contains(errorBlock, forbidden) {
			t.Fatalf("unexpected error field %s: %s", forbidden, errorBlock)
		}
	}
}

func TestThreadRunStreamEmitsClarificationRequest(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = &fakeClarificationLLMProvider{}
	s.tools = tools.NewRegistry()
	if err := s.tools.Register(clarification.AskClarificationTool(s.clarify)); err != nil {
		t.Fatalf("register clarification tool: %v", err)
	}

	reqBody := `{"assistant_id":"lead_agent","stream_mode":["events"],"input":{"messages":[{"role":"user","content":"ambiguous request"}]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-clarify/runs/stream", strings.NewReader(reqBody))
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
	clarifyBlock := sseEventBlock(t, text, "clarification_request")
	if !strings.Contains(text, "event: clarification_request") {
		t.Fatalf("missing clarification_request event: %s", text)
	}
	if !strings.Contains(text, `"thread_id":"thread-stream-clarify"`) {
		t.Fatalf("missing thread_id in clarification payload: %s", text)
	}
	if !strings.Contains(text, `"question":"Need more detail?"`) {
		t.Fatalf("missing clarification question: %s", text)
	}
	if !strings.Contains(text, `"type":"text"`) {
		t.Fatalf("missing clarification type: %s", text)
	}
	if !strings.Contains(text, `"id":"`) || !strings.Contains(text, `"required":true`) {
		t.Fatalf("missing clarification identity payload: %s", text)
	}
	for _, forbidden := range []string{`"run_id":`, `"assistant_id":`, `"messages":`, `"usage_metadata":`, `"tool_calls":`, `"tool_call_id":`, `"retryable":`} {
		if strings.Contains(clarifyBlock, forbidden) {
			t.Fatalf("unexpected clarification field %s: %s", forbidden, clarifyBlock)
		}
	}
}

func TestThreadRunStreamPersistsMessagesAcrossReload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	reqBody := `{"assistant_id":"lead_agent","messages":[{"id":"user-top","role":"user","content":"hello from top level"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-reload/runs/stream", strings.NewReader(reqBody))
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
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}

	loaded := &Server{
		dataRoot: s.dataRoot,
		sessions: map[string]*Session{},
	}
	loaded.loadPersistedThreads()

	session := loaded.sessions["thread-stream-reload"]
	if session == nil {
		t.Fatal("expected thread restored")
	}
	if len(session.Messages) < 2 {
		t.Fatalf("messages=%#v", session.Messages)
	}
	if session.Messages[0].ID != "user-top" || session.Messages[0].Content != "hello from top level" {
		t.Fatalf("message=%#v", session.Messages[0])
	}
	if session.Messages[len(session.Messages)-1].Content != "hello from fake llm" {
		t.Fatalf("last message=%#v", session.Messages[len(session.Messages)-1])
	}
}

func TestThreadRunStreamUpdatesHistoryMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	reqBody := `{"assistant_id":"lead_agent","messages":[{"id":"user-top","role":"user","content":"hello from top level"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/threads/thread-stream-history/runs/stream", strings.NewReader(reqBody))
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
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}

	historyResp, err := http.Get(ts.URL + "/threads/thread-stream-history/history?limit=1")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	defer historyResp.Body.Close()
	if historyResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(historyResp.Body)
		t.Fatalf("status=%d body=%s", historyResp.StatusCode, string(b))
	}

	var history []ThreadState
	if err := json.NewDecoder(historyResp.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history len=%d", len(history))
	}
	messages, _ := history[0].Values["messages"].([]any)
	if len(messages) < 2 {
		t.Fatalf("values=%#v", history[0].Values)
	}
	first, _ := messages[0].(map[string]any)
	last, _ := messages[len(messages)-1].(map[string]any)
	if first["id"] != "user-top" || first["content"] != "hello from top level" {
		t.Fatalf("first message=%#v", first)
	}
	if last["content"] != "hello from fake llm" {
		t.Fatalf("last message=%#v", last)
	}
}

func TestRunsStreamAcceptsCamelCaseIDs(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	reqBody := `{"assistantId":"lead_agent","threadId":"thread-camel-ids","streamMode":["values"],"input":{"messages":[{"role":"user","content":"hello"}]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/runs/stream", strings.NewReader(reqBody))
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

	threadResp, err := http.Get(ts.URL + "/threads/thread-camel-ids")
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	defer threadResp.Body.Close()
	if threadResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(threadResp.Body)
		t.Fatalf("thread status=%d body=%s", threadResp.StatusCode, string(b))
	}
	var thread map[string]any
	if err := json.NewDecoder(threadResp.Body).Decode(&thread); err != nil {
		t.Fatalf("decode thread: %v", err)
	}
	metadata, ok := thread["metadata"].(map[string]any)
	if !ok || metadata["assistant_id"] != "lead_agent" {
		t.Fatalf("metadata=%#v", thread["metadata"])
	}
	config, ok := thread["config"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", thread["config"])
	}
	configurable, ok := config["configurable"].(map[string]any)
	if !ok || configurable["thread_id"] != "thread-camel-ids" {
		t.Fatalf("configurable=%#v", config["configurable"])
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

func TestStreamModeFilterEventAliases(t *testing.T) {
	for _, mode := range []string{"events", "debug", "custom"} {
		filter := newStreamModeFilter([]any{mode})
		if !filter.allows("metadata") || !filter.allows("tool_call") || !filter.allows("error") {
			t.Fatalf("%s mode should allow core event stream", mode)
		}
		if !filter.allows("task_started") || !filter.allows("clarification_request") {
			t.Fatalf("%s mode should allow task and clarification events", mode)
		}
		if filter.allows("values") || filter.allows("updates") {
			t.Fatalf("%s mode should not implicitly allow values/updates", mode)
		}
		if !filter.allows("end") {
			t.Fatalf("%s mode should allow end", mode)
		}
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
			{ID: "1", Event: "metadata", Data: map[string]any{"run_id": "run-replay-1", "thread_id": "thread-replay-1", "assistant_id": "lead_agent"}},
			{ID: "2", Event: "messages-tuple", Data: map[string]any{"type": "ai", "content": "hello"}},
			{ID: "3", Event: "values", Data: map[string]any{"title": "done", "messages": []any{map[string]any{"id": "ai-1", "type": "ai", "content": "hello"}}, "artifacts": []any{"/tmp/report.md"}, "todos": []any{map[string]any{"content": "ship sqlite", "status": "pending"}}, "sandbox": map[string]any{"sandbox_id": "sb-1"}, "thread_data": map[string]any{"workspace_path": "/tmp/workspace"}, "uploaded_files": []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt"}}, "viewed_images": map[string]any{"/tmp/chart.png": map[string]any{"mime_type": "image/png"}}}},
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
	valuesBlock := sseEventBlock(t, text, "values")
	if strings.Contains(text, "event: metadata") {
		t.Fatalf("unexpected metadata event: %s", text)
	}
	if !strings.Contains(text, "event: values") {
		t.Fatalf("missing values event: %s", text)
	}
	if !strings.Contains(text, `"title":"done"`) {
		t.Fatalf("missing values payload: %s", text)
	}
	if !strings.Contains(text, `"messages":[{`) || !strings.Contains(text, `"id":"ai-1"`) || !strings.Contains(text, `"content":"hello"`) || !strings.Contains(text, `"type":"ai"`) {
		t.Fatalf("missing values messages payload: %s", text)
	}
	if !strings.Contains(text, `"artifacts":["/tmp/report.md"]`) {
		t.Fatalf("missing values artifacts payload: %s", text)
	}
	if !strings.Contains(text, `"todos":[{`) || !strings.Contains(text, `"content":"ship sqlite"`) || !strings.Contains(text, `"status":"pending"`) {
		t.Fatalf("missing values todos payload: %s", text)
	}
	if !strings.Contains(text, `"sandbox":{"sandbox_id":"sb-1"}`) {
		t.Fatalf("missing values sandbox payload: %s", text)
	}
	if !strings.Contains(text, `"thread_data":{"workspace_path":"/tmp/workspace"}`) {
		t.Fatalf("missing values thread_data payload: %s", text)
	}
	if !strings.Contains(text, `"uploaded_files":[{`) || !strings.Contains(text, `"filename":"notes.txt"`) || !strings.Contains(text, `"path":"/tmp/uploads/notes.txt"`) {
		t.Fatalf("missing values uploaded_files payload: %s", text)
	}
	if !strings.Contains(text, `"viewed_images":{"/tmp/chart.png":{"mime_type":"image/png"}}`) {
		t.Fatalf("missing values viewed_images payload: %s", text)
	}
	for _, forbidden := range []string{`"metadata":`, `"config":`, `"next":`, `"tasks":`, `"interrupts":`, `"checkpoint":`, `"parent_checkpoint":`} {
		if strings.Contains(valuesBlock, forbidden) {
			t.Fatalf("unexpected extra values payload field %s: %s", forbidden, valuesBlock)
		}
	}
	if strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("unexpected messages-tuple event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
}

func TestRecordedRunStreamReplaysMetadataPayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-meta",
		ThreadID:    "thread-replay-meta",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "metadata", Data: map[string]any{"run_id": "run-replay-meta", "thread_id": "thread-replay-meta", "assistant_id": "lead_agent"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-meta"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-meta/runs/run-replay-meta/stream")
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
	if !strings.Contains(text, "event: metadata") {
		t.Fatalf("missing metadata event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"run-replay-meta"`) || !strings.Contains(text, `"thread_id":"thread-replay-meta"`) {
		t.Fatalf("missing metadata IDs: %s", text)
	}
	if !strings.Contains(text, `"assistant_id":"lead_agent"`) {
		t.Fatalf("missing assistant_id in metadata: %s", text)
	}
	if strings.Contains(text, `"status":`) {
		t.Fatalf("unexpected status in metadata payload: %s", text)
	}
}

func TestRecordedRunStreamReplaysEndUsagePayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-end-usage",
		ThreadID:    "thread-replay-end-usage",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "end", Data: map[string]any{"run_id": "run-replay-end-usage", "usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-end-usage/runs/run-replay-end-usage/stream")
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
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"run-replay-end-usage"`) {
		t.Fatalf("missing run_id in end payload: %s", text)
	}
	if !strings.Contains(text, `"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}`) {
		t.Fatalf("missing usage in end payload: %s", text)
	}
	if strings.Contains(text, `"thread_id":"thread-replay-end-usage"`) || strings.Contains(text, `"assistant_id":"lead_agent"`) {
		t.Fatalf("unexpected extra fields in end payload: %s", text)
	}
}

func TestRecordedRunStreamReplaysToolMessageTuple(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-tool-message",
		ThreadID:    "thread-replay-tool-message",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "tool", "id": "tool:call-1", "tool_call_id": "call-1", "status": "success", "content": "done", "data": map[string]any{"duration": "1.5s", "error": "", "data": map[string]any{"path": "/tmp/demo.txt"}}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-tool-message"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-tool-message/runs/run-replay-tool-message/stream?streamMode=messages-tuple")
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
	tupleBlock := sseEventBlock(t, text, "messages-tuple")
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, `"tool_call_id":"call-1"`) || !strings.Contains(text, `"status":"success"`) {
		t.Fatalf("missing tool message tuple payload: %s", text)
	}
	if !strings.Contains(text, `"duration":"1.5s"`) || !strings.Contains(text, `"error":""`) {
		t.Fatalf("missing tool message tuple data payload: %s", text)
	}
	if !strings.Contains(text, `"data":{"path":"/tmp/demo.txt"}`) {
		t.Fatalf("missing tool message tuple nested data payload: %s", text)
	}
	for _, forbidden := range []string{`"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`} {
		if strings.Contains(tupleBlock, forbidden) {
			t.Fatalf("unexpected tool tuple field %s: %s", forbidden, tupleBlock)
		}
	}
}

func TestRecordedRunStreamReplaysAIUsageTuple(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-ai-usage",
		ThreadID:    "thread-replay-ai-usage",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "ai", "id": "ai-1", "content": "done", "usage_metadata": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-ai-usage"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-ai-usage/runs/run-replay-ai-usage/stream?streamMode=messages-tuple")
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
	tupleBlock := sseEventBlock(t, text, "messages-tuple")
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, `"id":"ai-1"`) {
		t.Fatalf("missing ai message id in tuple payload: %s", text)
	}
	if !strings.Contains(text, `"usage_metadata":{"input_tokens":10,"output_tokens":5,"total_tokens":15}`) {
		t.Fatalf("missing ai usage tuple payload: %s", text)
	}
	for _, forbidden := range []string{`"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"status":`, `"data":{`} {
		if strings.Contains(tupleBlock, forbidden) {
			t.Fatalf("unexpected ai usage tuple field %s: %s", forbidden, tupleBlock)
		}
	}
}

func TestRecordedRunStreamReplaysAIAdditionalKwargsTuple(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-ai-kwargs",
		ThreadID:    "thread-replay-ai-kwargs",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "ai", "id": "ai-1", "content": "done", "additional_kwargs": map[string]any{"reasoning_content": "Need to inspect report", "stop_reason": "end_turn"}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-ai-kwargs"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-ai-kwargs/runs/run-replay-ai-kwargs/stream?streamMode=messages-tuple")
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
	tupleBlock := sseEventBlock(t, text, "messages-tuple")
	if !strings.Contains(text, `"additional_kwargs":{"reasoning_content":"Need to inspect report","stop_reason":"end_turn"}`) {
		t.Fatalf("missing ai additional_kwargs tuple payload: %s", text)
	}
	if !strings.Contains(text, `"id":"ai-1"`) {
		t.Fatalf("missing ai message id in tuple payload: %s", text)
	}
	for _, forbidden := range []string{`"usage_metadata":`, `"tool_calls":`, `"tool_call_id":`, `"status":`, `"data":{`} {
		if strings.Contains(tupleBlock, forbidden) {
			t.Fatalf("unexpected ai kwargs tuple field %s: %s", forbidden, tupleBlock)
		}
	}
}

func TestRecordedRunStreamReplaysAIToolCallsTuple(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-ai-tool-calls",
		ThreadID:    "thread-replay-ai-tool-calls",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "ai", "id": "ai-1", "tool_calls": []any{map[string]any{"id": "call-1", "name": "read_file", "args": map[string]any{"path": "/tmp/demo.txt"}}}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-ai-tool-calls"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-ai-tool-calls/runs/run-replay-ai-tool-calls/stream?streamMode=messages-tuple")
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
	tupleBlock := sseEventBlock(t, text, "messages-tuple")
	if !strings.Contains(text, `"tool_calls":[{`) || !strings.Contains(text, `"id":"call-1"`) || !strings.Contains(text, `"name":"read_file"`) || !strings.Contains(text, `"args":{"path":"/tmp/demo.txt"}`) {
		t.Fatalf("missing ai tool_calls tuple payload: %s", text)
	}
	for _, forbidden := range []string{`"usage_metadata":`, `"additional_kwargs":`, `"tool_call_id":`, `"status":`, `"data":{`} {
		if strings.Contains(tupleBlock, forbidden) {
			t.Fatalf("unexpected ai tool_calls tuple field %s: %s", forbidden, tupleBlock)
		}
	}
}

func TestRecordedRunStreamReplaysToolCallEvents(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-tools",
		ThreadID:    "thread-replay-tools",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "tool_call_start", Data: map[string]any{"id": "call-1", "name": "read_file"}},
			{ID: "2", Event: "tool_call_end", Data: map[string]any{"id": "call-1", "name": "read_file"}},
			{ID: "3", Event: "on_tool_end", Data: map[string]any{"event": "on_tool_end", "name": "read_file", "data": map[string]any{"id": "call-1"}}},
			{ID: "4", Event: "end", Data: map[string]any{"run_id": "run-replay-tools"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-tools/runs/run-replay-tools/stream?streamMode=events")
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
	startBlock := sseEventBlock(t, text, "tool_call_start")
	endBlock := sseEventBlock(t, text, "tool_call_end")
	aliasBlock := sseEventBlock(t, text, "on_tool_end")
	if !strings.Contains(text, "event: tool_call_start") {
		t.Fatalf("missing tool_call_start event: %s", text)
	}
	if !strings.Contains(text, "event: tool_call_end") {
		t.Fatalf("missing tool_call_end event: %s", text)
	}
	if !strings.Contains(text, "event: on_tool_end") {
		t.Fatalf("missing on_tool_end event: %s", text)
	}
	if !strings.Contains(text, `"id":"call-1"`) {
		t.Fatalf("missing tool call id: %s", text)
	}
	if !strings.Contains(startBlock, `"name":"read_file"`) || !strings.Contains(endBlock, `"name":"read_file"`) {
		t.Fatalf("missing tool call name in start/end payloads: %s %s", startBlock, endBlock)
	}
	if !strings.Contains(aliasBlock, `"name":"read_file"`) || !strings.Contains(aliasBlock, `"data":{"id":"call-1"`) {
		t.Fatalf("missing on_tool_end payload shape: %s", aliasBlock)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`} {
		if strings.Contains(startBlock, forbidden) {
			t.Fatalf("unexpected tool_call_start field %s: %s", forbidden, startBlock)
		}
		if strings.Contains(endBlock, forbidden) {
			t.Fatalf("unexpected tool_call_end field %s: %s", forbidden, endBlock)
		}
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`, `"status":`} {
		if strings.Contains(aliasBlock, forbidden) {
			t.Fatalf("unexpected on_tool_end field %s: %s", forbidden, aliasBlock)
		}
	}
}

func TestRecordedRunStreamReplaysToolCallEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-tool-call",
		ThreadID:    "thread-replay-tool-call",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "tool_call", Data: map[string]any{"id": "call-1", "name": "read_file"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-tool-call"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-tool-call/runs/run-replay-tool-call/stream?streamMode=events")
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
	callBlock := sseEventBlock(t, text, "tool_call")
	if !strings.Contains(text, "event: tool_call") {
		t.Fatalf("missing tool_call event: %s", text)
	}
	if !strings.Contains(text, `"id":"call-1"`) || !strings.Contains(text, `"name":"read_file"`) {
		t.Fatalf("missing tool_call payload: %s", text)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`, `"status":`, `"data":{`} {
		if strings.Contains(callBlock, forbidden) {
			t.Fatalf("unexpected tool_call field %s: %s", forbidden, callBlock)
		}
	}
}

func TestRecordedRunStreamReplaysChunkEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-chunk",
		ThreadID:    "thread-replay-chunk",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "chunk", Data: map[string]any{"delta": "hello from replay", "content": "hello from replay", "type": "ai"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-chunk"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-chunk/runs/run-replay-chunk/stream?streamMode=events")
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
	chunkBlock := sseEventBlock(t, text, "chunk")
	if !strings.Contains(text, "event: chunk") {
		t.Fatalf("missing chunk event: %s", text)
	}
	if !strings.Contains(text, `"delta":"hello from replay"`) {
		t.Fatalf("missing chunk payload: %s", text)
	}
	if !strings.Contains(text, `"content":"hello from replay"`) {
		t.Fatalf("missing chunk content payload: %s", text)
	}
	if !strings.Contains(chunkBlock, `"type":"ai"`) {
		t.Fatalf("missing replay chunk type payload: %s", chunkBlock)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"status":`, `"data":{`, `"role":`, `"thread_id":`, `"run_id":`} {
		if strings.Contains(chunkBlock, forbidden) {
			t.Fatalf("unexpected replay chunk field %s: %s", forbidden, chunkBlock)
		}
	}
}

func TestRecordedRunStreamReplaysErrorEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-error",
		ThreadID:    "thread-replay-error",
		AssistantID: "lead_agent",
		Status:      "error",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "error", Data: map[string]any{"error": "RunError", "name": "RunError", "message": "boom", "suggestion": "retry", "retryable": true}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-error"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-error/runs/run-replay-error/stream?streamMode=events")
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
	errorBlock := sseEventBlock(t, text, "error")
	if !strings.Contains(text, "event: error") {
		t.Fatalf("missing error event: %s", text)
	}
	if !strings.Contains(text, `"message":"boom"`) || !strings.Contains(text, `"suggestion":"retry"`) {
		t.Fatalf("missing error payload: %s", text)
	}
	if !strings.Contains(text, `"error":"RunError"`) || !strings.Contains(text, `"name":"RunError"`) {
		t.Fatalf("missing replay error identity: %s", text)
	}
	if !strings.Contains(text, `"retryable":true`) {
		t.Fatalf("missing retryable flag: %s", text)
	}
	for _, forbidden := range []string{`"thread_id":`, `"run_id":`, `"assistant_id":`, `"messages":`, `"usage_metadata":`, `"tool_calls":`, `"tool_call_id":`} {
		if strings.Contains(errorBlock, forbidden) {
			t.Fatalf("unexpected error field %s: %s", forbidden, errorBlock)
		}
	}
}

func TestRecordedRunStreamReplaysClarificationRequest(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-clarify",
		ThreadID:    "thread-replay-clarify",
		AssistantID: "lead_agent",
		Status:      "interrupted",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "clarification_request", Data: map[string]any{"id": "clarify-1", "thread_id": "thread-replay-clarify", "type": "text", "question": "Need more detail?", "required": true}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-clarify"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-clarify/runs/run-replay-clarify/stream?streamMode=events")
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
	clarifyBlock := sseEventBlock(t, text, "clarification_request")
	if !strings.Contains(text, "event: clarification_request") {
		t.Fatalf("missing clarification_request event: %s", text)
	}
	if !strings.Contains(text, `"thread_id":"thread-replay-clarify"`) {
		t.Fatalf("missing thread_id in clarification payload: %s", text)
	}
	if !strings.Contains(text, `"question":"Need more detail?"`) {
		t.Fatalf("missing clarification question: %s", text)
	}
	if !strings.Contains(text, `"id":"clarify-1"`) || !strings.Contains(text, `"type":"text"`) || !strings.Contains(text, `"required":true`) {
		t.Fatalf("missing replay clarification identity: %s", text)
	}
	for _, forbidden := range []string{`"run_id":`, `"assistant_id":`, `"messages":`, `"usage_metadata":`, `"tool_calls":`, `"tool_call_id":`, `"retryable":`} {
		if strings.Contains(clarifyBlock, forbidden) {
			t.Fatalf("unexpected clarification field %s: %s", forbidden, clarifyBlock)
		}
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
			{ID: "1", Event: "metadata", Data: map[string]any{"run_id": "run-join-1", "thread_id": "thread-join-1", "assistant_id": "lead_agent"}},
			{ID: "2", Event: "messages-tuple", Data: map[string]any{"type": "ai", "content": "hello"}},
			{ID: "3", Event: "values", Data: map[string]any{"title": "done"}},
			{ID: "4", Event: "end", Data: map[string]any{"run_id": "run-join-1"}},
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
	if strings.Contains(text, "event: metadata") {
		t.Fatalf("unexpected metadata event: %s", text)
	}
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, `"content":"hello"`) {
		t.Fatalf("missing message payload: %s", text)
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"run-join-1"`) {
		t.Fatalf("missing run_id in end payload: %s", text)
	}
}

func TestThreadJoinStreamReplaysMetadataPayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-meta",
		ThreadID:    "thread-join-meta",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "metadata", Data: map[string]any{"run_id": "run-join-meta", "thread_id": "thread-join-meta", "assistant_id": "lead_agent"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-meta"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-meta/stream")
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
	if !strings.Contains(text, "event: metadata") {
		t.Fatalf("missing metadata event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"run-join-meta"`) || !strings.Contains(text, `"thread_id":"thread-join-meta"`) {
		t.Fatalf("missing metadata IDs: %s", text)
	}
	if !strings.Contains(text, `"assistant_id":"lead_agent"`) {
		t.Fatalf("missing assistant_id in metadata: %s", text)
	}
	if strings.Contains(text, `"status":`) {
		t.Fatalf("unexpected status in metadata payload: %s", text)
	}
}

func TestThreadJoinStreamReplaysEndUsagePayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-end-usage",
		ThreadID:    "thread-join-end-usage",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "end", Data: map[string]any{"run_id": "run-join-end-usage", "usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-end-usage/stream")
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
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"run-join-end-usage"`) {
		t.Fatalf("missing run_id in end payload: %s", text)
	}
	if !strings.Contains(text, `"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}`) {
		t.Fatalf("missing usage in end payload: %s", text)
	}
	if strings.Contains(text, `"thread_id":"thread-join-end-usage"`) || strings.Contains(text, `"assistant_id":"lead_agent"`) {
		t.Fatalf("unexpected extra fields in end payload: %s", text)
	}
}

func TestThreadJoinStreamReplaysToolMessageTuple(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-tool-message",
		ThreadID:    "thread-join-tool-message",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "tool", "id": "tool:call-1", "tool_call_id": "call-1", "status": "success", "content": "done", "data": map[string]any{"duration": "1.5s", "error": "", "data": map[string]any{"path": "/tmp/demo.txt"}}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-tool-message"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-tool-message/stream?streamMode=messages-tuple")
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
	tupleBlock := sseEventBlock(t, text, "messages-tuple")
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, `"tool_call_id":"call-1"`) || !strings.Contains(text, `"status":"success"`) {
		t.Fatalf("missing tool message tuple payload: %s", text)
	}
	if !strings.Contains(text, `"duration":"1.5s"`) || !strings.Contains(text, `"error":""`) {
		t.Fatalf("missing tool message tuple data payload: %s", text)
	}
	if !strings.Contains(text, `"data":{"path":"/tmp/demo.txt"}`) {
		t.Fatalf("missing tool message tuple nested data payload: %s", text)
	}
	for _, forbidden := range []string{`"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`} {
		if strings.Contains(tupleBlock, forbidden) {
			t.Fatalf("unexpected tool tuple field %s: %s", forbidden, tupleBlock)
		}
	}
}

func TestThreadJoinStreamReplaysAIUsageTuple(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-ai-usage",
		ThreadID:    "thread-join-ai-usage",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "ai", "id": "ai-1", "content": "done", "usage_metadata": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-ai-usage"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-ai-usage/stream?streamMode=messages-tuple")
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
	tupleBlock := sseEventBlock(t, text, "messages-tuple")
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, `"id":"ai-1"`) {
		t.Fatalf("missing ai message id in tuple payload: %s", text)
	}
	if !strings.Contains(text, `"usage_metadata":{"input_tokens":10,"output_tokens":5,"total_tokens":15}`) {
		t.Fatalf("missing ai usage tuple payload: %s", text)
	}
	for _, forbidden := range []string{`"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"status":`, `"data":{`} {
		if strings.Contains(tupleBlock, forbidden) {
			t.Fatalf("unexpected ai usage tuple field %s: %s", forbidden, tupleBlock)
		}
	}
}

func TestThreadJoinStreamReplaysAIAdditionalKwargsTuple(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-ai-kwargs",
		ThreadID:    "thread-join-ai-kwargs",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "ai", "id": "ai-1", "content": "done", "additional_kwargs": map[string]any{"reasoning_content": "Need to inspect report", "stop_reason": "end_turn"}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-ai-kwargs"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-ai-kwargs/stream?streamMode=messages-tuple")
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
	tupleBlock := sseEventBlock(t, text, "messages-tuple")
	if !strings.Contains(text, `"additional_kwargs":{"reasoning_content":"Need to inspect report","stop_reason":"end_turn"}`) {
		t.Fatalf("missing ai additional_kwargs tuple payload: %s", text)
	}
	if !strings.Contains(text, `"id":"ai-1"`) {
		t.Fatalf("missing ai message id in tuple payload: %s", text)
	}
	for _, forbidden := range []string{`"usage_metadata":`, `"tool_calls":`, `"tool_call_id":`, `"status":`, `"data":{`} {
		if strings.Contains(tupleBlock, forbidden) {
			t.Fatalf("unexpected ai kwargs tuple field %s: %s", forbidden, tupleBlock)
		}
	}
}

func TestThreadJoinStreamReplaysAIToolCallsTuple(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-ai-tool-calls",
		ThreadID:    "thread-join-ai-tool-calls",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "messages-tuple", Data: map[string]any{"type": "ai", "id": "ai-1", "tool_calls": []any{map[string]any{"id": "call-1", "name": "read_file", "args": map[string]any{"path": "/tmp/demo.txt"}}}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-ai-tool-calls"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-ai-tool-calls/stream?streamMode=messages-tuple")
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
	tupleBlock := sseEventBlock(t, text, "messages-tuple")
	if !strings.Contains(text, `"tool_calls":[{`) || !strings.Contains(text, `"id":"call-1"`) || !strings.Contains(text, `"name":"read_file"`) || !strings.Contains(text, `"args":{"path":"/tmp/demo.txt"}`) {
		t.Fatalf("missing ai tool_calls tuple payload: %s", text)
	}
	for _, forbidden := range []string{`"usage_metadata":`, `"additional_kwargs":`, `"tool_call_id":`, `"status":`, `"data":{`} {
		if strings.Contains(tupleBlock, forbidden) {
			t.Fatalf("unexpected ai tool_calls tuple field %s: %s", forbidden, tupleBlock)
		}
	}
}

func TestThreadJoinStreamReplaysValuesPayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-values",
		ThreadID:    "thread-join-values",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "values", Data: map[string]any{"title": "done", "messages": []any{map[string]any{"id": "ai-1", "type": "ai", "content": "hello"}}, "artifacts": []any{"/tmp/report.md"}, "todos": []any{map[string]any{"content": "ship sqlite", "status": "pending"}}, "sandbox": map[string]any{"sandbox_id": "sb-1"}, "thread_data": map[string]any{"workspace_path": "/tmp/workspace"}, "uploaded_files": []any{map[string]any{"filename": "notes.txt", "path": "/tmp/uploads/notes.txt"}}, "viewed_images": map[string]any{"/tmp/chart.png": map[string]any{"mime_type": "image/png"}}}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-values"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-values/stream")
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
	valuesBlock := sseEventBlock(t, text, "values")
	if !strings.Contains(text, "event: values") {
		t.Fatalf("missing values event: %s", text)
	}
	if !strings.Contains(text, `"title":"done"`) {
		t.Fatalf("missing values payload: %s", text)
	}
	if !strings.Contains(text, `"messages":[{`) || !strings.Contains(text, `"id":"ai-1"`) || !strings.Contains(text, `"content":"hello"`) || !strings.Contains(text, `"type":"ai"`) {
		t.Fatalf("missing values messages payload: %s", text)
	}
	if !strings.Contains(text, `"artifacts":["/tmp/report.md"]`) {
		t.Fatalf("missing values artifacts payload: %s", text)
	}
	if !strings.Contains(text, `"todos":[{`) || !strings.Contains(text, `"content":"ship sqlite"`) || !strings.Contains(text, `"status":"pending"`) {
		t.Fatalf("missing values todos payload: %s", text)
	}
	if !strings.Contains(text, `"sandbox":{"sandbox_id":"sb-1"}`) {
		t.Fatalf("missing values sandbox payload: %s", text)
	}
	if !strings.Contains(text, `"thread_data":{"workspace_path":"/tmp/workspace"}`) {
		t.Fatalf("missing values thread_data payload: %s", text)
	}
	if !strings.Contains(text, `"uploaded_files":[{`) || !strings.Contains(text, `"filename":"notes.txt"`) || !strings.Contains(text, `"path":"/tmp/uploads/notes.txt"`) {
		t.Fatalf("missing values uploaded_files payload: %s", text)
	}
	if !strings.Contains(text, `"viewed_images":{"/tmp/chart.png":{"mime_type":"image/png"}}`) {
		t.Fatalf("missing values viewed_images payload: %s", text)
	}
	for _, forbidden := range []string{`"metadata":`, `"config":`, `"next":`, `"tasks":`, `"interrupts":`, `"checkpoint":`, `"parent_checkpoint":`} {
		if strings.Contains(valuesBlock, forbidden) {
			t.Fatalf("unexpected extra values payload field %s: %s", forbidden, valuesBlock)
		}
	}
}

func TestThreadJoinStreamReplaysToolCallEvents(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-tools",
		ThreadID:    "thread-join-tools",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "tool_call_start", Data: map[string]any{"id": "call-1", "name": "read_file"}},
			{ID: "2", Event: "tool_call_end", Data: map[string]any{"id": "call-1", "name": "read_file"}},
			{ID: "3", Event: "on_tool_end", Data: map[string]any{"event": "on_tool_end", "name": "read_file", "data": map[string]any{"id": "call-1"}}},
			{ID: "4", Event: "end", Data: map[string]any{"run_id": "run-join-tools"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-tools/stream?streamMode=events")
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
	startBlock := sseEventBlock(t, text, "tool_call_start")
	endBlock := sseEventBlock(t, text, "tool_call_end")
	aliasBlock := sseEventBlock(t, text, "on_tool_end")
	if !strings.Contains(text, "event: tool_call_start") {
		t.Fatalf("missing tool_call_start event: %s", text)
	}
	if !strings.Contains(text, "event: tool_call_end") {
		t.Fatalf("missing tool_call_end event: %s", text)
	}
	if !strings.Contains(text, "event: on_tool_end") {
		t.Fatalf("missing on_tool_end event: %s", text)
	}
	if !strings.Contains(text, `"id":"call-1"`) {
		t.Fatalf("missing tool call id: %s", text)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`} {
		if strings.Contains(startBlock, forbidden) {
			t.Fatalf("unexpected tool_call_start field %s: %s", forbidden, startBlock)
		}
		if strings.Contains(endBlock, forbidden) {
			t.Fatalf("unexpected tool_call_end field %s: %s", forbidden, endBlock)
		}
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`, `"status":`} {
		if strings.Contains(aliasBlock, forbidden) {
			t.Fatalf("unexpected on_tool_end field %s: %s", forbidden, aliasBlock)
		}
	}
}

func TestThreadJoinStreamReplaysToolCallEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-tool-call",
		ThreadID:    "thread-join-tool-call",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "tool_call", Data: map[string]any{"id": "call-1", "name": "read_file"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-tool-call"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-tool-call/stream?streamMode=events")
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
	callBlock := sseEventBlock(t, text, "tool_call")
	if !strings.Contains(text, "event: tool_call") {
		t.Fatalf("missing tool_call event: %s", text)
	}
	if !strings.Contains(text, `"id":"call-1"`) || !strings.Contains(text, `"name":"read_file"`) {
		t.Fatalf("missing tool_call payload: %s", text)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"thread_id":`, `"run_id":`, `"status":`, `"data":{`} {
		if strings.Contains(callBlock, forbidden) {
			t.Fatalf("unexpected tool_call field %s: %s", forbidden, callBlock)
		}
	}
}

func TestThreadJoinStreamReplaysChunkEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-chunk",
		ThreadID:    "thread-join-chunk",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "chunk", Data: map[string]any{"delta": "hello from join", "content": "hello from join", "type": "ai"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-chunk"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-chunk/stream?streamMode=events")
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
	chunkBlock := sseEventBlock(t, text, "chunk")
	if !strings.Contains(text, "event: chunk") {
		t.Fatalf("missing chunk event: %s", text)
	}
	if !strings.Contains(text, `"delta":"hello from join"`) {
		t.Fatalf("missing chunk payload: %s", text)
	}
	if !strings.Contains(text, `"content":"hello from join"`) {
		t.Fatalf("missing chunk content payload: %s", text)
	}
	if !strings.Contains(chunkBlock, `"type":"ai"`) {
		t.Fatalf("missing join chunk type payload: %s", chunkBlock)
	}
	for _, forbidden := range []string{`"messages":`, `"usage_metadata":`, `"additional_kwargs":`, `"tool_calls":`, `"tool_call_id":`, `"status":`, `"data":{`, `"role":`, `"thread_id":`, `"run_id":`} {
		if strings.Contains(chunkBlock, forbidden) {
			t.Fatalf("unexpected join chunk field %s: %s", forbidden, chunkBlock)
		}
	}
}

func TestThreadJoinStreamReplaysErrorEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-error",
		ThreadID:    "thread-join-error",
		AssistantID: "lead_agent",
		Status:      "error",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "error", Data: map[string]any{"error": "RunError", "name": "RunError", "message": "boom", "suggestion": "retry", "retryable": true}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-error"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-error/stream?streamMode=events")
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
	errorBlock := sseEventBlock(t, text, "error")
	if !strings.Contains(text, "event: error") {
		t.Fatalf("missing error event: %s", text)
	}
	if !strings.Contains(text, `"message":"boom"`) || !strings.Contains(text, `"suggestion":"retry"`) {
		t.Fatalf("missing error payload: %s", text)
	}
	if !strings.Contains(text, `"error":"RunError"`) || !strings.Contains(text, `"name":"RunError"`) {
		t.Fatalf("missing join error identity: %s", text)
	}
	if !strings.Contains(text, `"retryable":true`) {
		t.Fatalf("missing retryable flag: %s", text)
	}
	for _, forbidden := range []string{`"thread_id":`, `"run_id":`, `"assistant_id":`, `"messages":`, `"usage_metadata":`, `"tool_calls":`, `"tool_call_id":`} {
		if strings.Contains(errorBlock, forbidden) {
			t.Fatalf("unexpected error field %s: %s", forbidden, errorBlock)
		}
	}
}

func TestThreadJoinStreamReplaysClarificationRequest(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-clarify",
		ThreadID:    "thread-join-clarify",
		AssistantID: "lead_agent",
		Status:      "interrupted",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "clarification_request", Data: map[string]any{"id": "clarify-1", "thread_id": "thread-join-clarify", "type": "text", "question": "Need more detail?", "required": true}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-clarify"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-clarify/stream?streamMode=events")
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
	clarifyBlock := sseEventBlock(t, text, "clarification_request")
	if !strings.Contains(text, "event: clarification_request") {
		t.Fatalf("missing clarification_request event: %s", text)
	}
	if !strings.Contains(text, `"thread_id":"thread-join-clarify"`) {
		t.Fatalf("missing thread_id in clarification payload: %s", text)
	}
	if !strings.Contains(text, `"question":"Need more detail?"`) {
		t.Fatalf("missing clarification question: %s", text)
	}
	if !strings.Contains(text, `"id":"clarify-1"`) || !strings.Contains(text, `"type":"text"`) || !strings.Contains(text, `"required":true`) {
		t.Fatalf("missing join clarification identity: %s", text)
	}
	for _, forbidden := range []string{`"run_id":`, `"assistant_id":`, `"messages":`, `"usage_metadata":`, `"tool_calls":`, `"tool_call_id":`, `"retryable":`} {
		if strings.Contains(clarifyBlock, forbidden) {
			t.Fatalf("unexpected clarification field %s: %s", forbidden, clarifyBlock)
		}
	}
}

func TestRecordedRunStreamModeSupportsUpdates(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-updates",
		ThreadID:    "thread-replay-updates",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "updates", Data: map[string]any{"agent": map[string]any{"title": "done", "messages": []any{map[string]any{"id": "ai-1", "type": "ai", "content": "hello"}}, "artifacts": []any{"/tmp/report.md"}}}},
			{ID: "2", Event: "messages-tuple", Data: map[string]any{"type": "ai", "content": "hello"}},
			{ID: "3", Event: "end", Data: map[string]any{"run_id": "run-replay-updates"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-updates/runs/run-replay-updates/stream?streamMode=updates")
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
	updatesBlock := sseEventBlock(t, text, "updates")
	if !strings.Contains(text, "event: updates") {
		t.Fatalf("missing updates event: %s", text)
	}
	if !strings.Contains(text, `"title":"done"`) {
		t.Fatalf("missing updates payload: %s", text)
	}
	if !strings.Contains(text, `"messages":[{`) || !strings.Contains(text, `"id":"ai-1"`) || !strings.Contains(text, `"artifacts":["/tmp/report.md"]`) {
		t.Fatalf("missing updates shape payload: %s", text)
	}
	if strings.Contains(text, `"values":`) {
		t.Fatalf("unexpected nested values payload: %s", text)
	}
	for _, forbidden := range []string{`"todos":`, `"sandbox":`, `"thread_data":`, `"uploaded_files":`, `"viewed_images":`, `"run_id":`, `"thread_id":`, `"assistant_id":`, `"metadata":`, `"config":`} {
		if strings.Contains(updatesBlock, forbidden) {
			t.Fatalf("unexpected extra updates payload field %s: %s", forbidden, updatesBlock)
		}
	}
	if strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("unexpected messages-tuple event: %s", text)
	}
}

func TestThreadJoinStreamModeSupportsUpdates(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-updates",
		ThreadID:    "thread-join-updates",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "updates", Data: map[string]any{"agent": map[string]any{"title": "done", "messages": []any{map[string]any{"id": "ai-1", "type": "ai", "content": "hello"}}, "artifacts": []any{"/tmp/report.md"}}}},
			{ID: "2", Event: "messages-tuple", Data: map[string]any{"type": "ai", "content": "hello"}},
			{ID: "3", Event: "end", Data: map[string]any{"run_id": "run-join-updates"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-updates/stream?stream_mode=updates")
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
	updatesBlock := sseEventBlock(t, text, "updates")
	if !strings.Contains(text, "event: updates") {
		t.Fatalf("missing updates event: %s", text)
	}
	if !strings.Contains(text, `"title":"done"`) {
		t.Fatalf("missing updates payload: %s", text)
	}
	if !strings.Contains(text, `"messages":[{`) || !strings.Contains(text, `"id":"ai-1"`) || !strings.Contains(text, `"artifacts":["/tmp/report.md"]`) {
		t.Fatalf("missing updates shape payload: %s", text)
	}
	if strings.Contains(text, `"values":`) {
		t.Fatalf("unexpected nested values payload: %s", text)
	}
	for _, forbidden := range []string{`"todos":`, `"sandbox":`, `"thread_data":`, `"uploaded_files":`, `"viewed_images":`} {
		if strings.Contains(updatesBlock, forbidden) {
			t.Fatalf("unexpected extra updates payload field %s: %s", forbidden, updatesBlock)
		}
	}
	if strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("unexpected messages-tuple event: %s", text)
	}
}

func TestRecordedRunStreamModeSupportsCommaSeparatedAliases(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-2",
		ThreadID:    "thread-replay-2",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "task_started", Data: map[string]any{"task_id": "t1"}},
			{ID: "2", Event: "messages-tuple", Data: map[string]any{"type": "ai", "content": "hello"}},
			{ID: "3", Event: "values", Data: map[string]any{"title": "done"}},
			{ID: "4", Event: "end", Data: map[string]any{"run_id": "run-replay-2"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-2/runs/run-replay-2/stream?streamMode=tasks,messages")
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
	taskBlock := sseEventBlock(t, text, "task_started")
	if !strings.Contains(text, "event: task_started") {
		t.Fatalf("missing task_started event: %s", text)
	}
	if !strings.Contains(text, `"task_id":"t1"`) {
		t.Fatalf("missing task payload: %s", text)
	}
	for _, forbidden := range []string{`"message":`, `"result":`, `"error":`, `"description":`} {
		if strings.Contains(taskBlock, forbidden) {
			t.Fatalf("unexpected task_started field %s: %s", forbidden, taskBlock)
		}
	}
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, `"content":"hello"`) {
		t.Fatalf("missing message payload: %s", text)
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"run-replay-2"`) {
		t.Fatalf("missing run_id in end payload: %s", text)
	}
}

func TestRecordedRunStreamReplaysTaskCompletedEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-task-complete",
		ThreadID:    "thread-replay-task-complete",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "task_completed", Data: map[string]any{"task_id": "t1", "result": "done"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-task-complete"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-task-complete/runs/run-replay-task-complete/stream?streamMode=tasks")
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
	taskBlock := sseEventBlock(t, text, "task_completed")
	if !strings.Contains(text, "event: task_completed") {
		t.Fatalf("missing task_completed event: %s", text)
	}
	if !strings.Contains(text, `"task_id":"t1"`) || !strings.Contains(text, `"result":"done"`) {
		t.Fatalf("missing task_completed payload: %s", text)
	}
	for _, forbidden := range []string{`"message":`, `"error":`, `"description":`} {
		if strings.Contains(taskBlock, forbidden) {
			t.Fatalf("unexpected task_completed field %s: %s", forbidden, taskBlock)
		}
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
}

func TestRecordedRunStreamReplaysTaskFailedEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-task-failed",
		ThreadID:    "thread-replay-task-failed",
		AssistantID: "lead_agent",
		Status:      "error",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "task_failed", Data: map[string]any{"task_id": "t1", "error": "boom"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-task-failed"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-task-failed/runs/run-replay-task-failed/stream?streamMode=tasks")
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
	taskBlock := sseEventBlock(t, text, "task_failed")
	if !strings.Contains(text, "event: task_failed") {
		t.Fatalf("missing task_failed event: %s", text)
	}
	if !strings.Contains(text, `"task_id":"t1"`) || !strings.Contains(text, `"error":"boom"`) {
		t.Fatalf("missing task_failed payload: %s", text)
	}
	for _, forbidden := range []string{`"message":`, `"result":`, `"description":`} {
		if strings.Contains(taskBlock, forbidden) {
			t.Fatalf("unexpected task_failed field %s: %s", forbidden, taskBlock)
		}
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
}

func TestRecordedRunStreamReplaysTaskRunningEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-replay-task-running",
		ThreadID:    "thread-replay-task-running",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "task_running", Data: map[string]any{"task_id": "t1", "message": "working"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-replay-task-running"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-replay-task-running/runs/run-replay-task-running/stream?streamMode=tasks")
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
	taskBlock := sseEventBlock(t, text, "task_running")
	if !strings.Contains(text, "event: task_running") {
		t.Fatalf("missing task_running event: %s", text)
	}
	if !strings.Contains(text, `"task_id":"t1"`) || !strings.Contains(text, `"message":"working"`) {
		t.Fatalf("missing task_running payload: %s", text)
	}
	for _, forbidden := range []string{`"error":`, `"result":`, `"description":`} {
		if strings.Contains(taskBlock, forbidden) {
			t.Fatalf("unexpected task_running field %s: %s", forbidden, taskBlock)
		}
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
}

func TestThreadJoinStreamModeSupportsCommaSeparatedAliases(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-2",
		ThreadID:    "thread-join-2",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "task_started", Data: map[string]any{"task_id": "t1"}},
			{ID: "2", Event: "messages-tuple", Data: map[string]any{"type": "ai", "content": "hello"}},
			{ID: "3", Event: "values", Data: map[string]any{"title": "done"}},
			{ID: "4", Event: "end", Data: map[string]any{"run_id": "run-join-2"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-2/stream?stream_mode=tasks,messages")
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
	taskBlock := sseEventBlock(t, text, "task_started")
	if !strings.Contains(text, "event: task_started") {
		t.Fatalf("missing task_started event: %s", text)
	}
	if !strings.Contains(text, `"task_id":"t1"`) {
		t.Fatalf("missing task payload: %s", text)
	}
	for _, forbidden := range []string{`"message":`, `"result":`, `"error":`, `"description":`} {
		if strings.Contains(taskBlock, forbidden) {
			t.Fatalf("unexpected task_started field %s: %s", forbidden, taskBlock)
		}
	}
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, `"content":"hello"`) {
		t.Fatalf("missing message payload: %s", text)
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
	if !strings.Contains(text, `"run_id":"run-join-2"`) {
		t.Fatalf("missing run_id in end payload: %s", text)
	}
}

func TestThreadJoinStreamReplaysTaskCompletedEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-task-complete",
		ThreadID:    "thread-join-task-complete",
		AssistantID: "lead_agent",
		Status:      "success",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "task_completed", Data: map[string]any{"task_id": "t1", "result": "done"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-task-complete"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-task-complete/stream?streamMode=tasks")
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
	taskBlock := sseEventBlock(t, text, "task_completed")
	if !strings.Contains(text, "event: task_completed") {
		t.Fatalf("missing task_completed event: %s", text)
	}
	if !strings.Contains(text, `"task_id":"t1"`) || !strings.Contains(text, `"result":"done"`) {
		t.Fatalf("missing task_completed payload: %s", text)
	}
	for _, forbidden := range []string{`"message":`, `"error":`, `"description":`} {
		if strings.Contains(taskBlock, forbidden) {
			t.Fatalf("unexpected task_completed field %s: %s", forbidden, taskBlock)
		}
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
}

func TestThreadJoinStreamReplaysTaskFailedEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-task-failed",
		ThreadID:    "thread-join-task-failed",
		AssistantID: "lead_agent",
		Status:      "error",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "task_failed", Data: map[string]any{"task_id": "t1", "error": "boom"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-task-failed"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-task-failed/stream?streamMode=tasks")
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
	taskBlock := sseEventBlock(t, text, "task_failed")
	if !strings.Contains(text, "event: task_failed") {
		t.Fatalf("missing task_failed event: %s", text)
	}
	if !strings.Contains(text, `"task_id":"t1"`) || !strings.Contains(text, `"error":"boom"`) {
		t.Fatalf("missing task_failed payload: %s", text)
	}
	for _, forbidden := range []string{`"message":`, `"result":`, `"description":`} {
		if strings.Contains(taskBlock, forbidden) {
			t.Fatalf("unexpected task_failed field %s: %s", forbidden, taskBlock)
		}
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
	}
}

func TestThreadJoinStreamReplaysTaskRunningEvent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	run := &Run{
		RunID:       "run-join-task-running",
		ThreadID:    "thread-join-task-running",
		AssistantID: "lead_agent",
		Status:      "running",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Events: []StreamEvent{
			{ID: "1", Event: "task_running", Data: map[string]any{"task_id": "t1", "message": "working"}},
			{ID: "2", Event: "end", Data: map[string]any{"run_id": "run-join-task-running"}},
		},
	}
	s.saveRun(run)

	resp, err := http.Get(ts.URL + "/threads/thread-join-task-running/stream?streamMode=tasks")
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
	taskBlock := sseEventBlock(t, text, "task_running")
	if !strings.Contains(text, "event: task_running") {
		t.Fatalf("missing task_running event: %s", text)
	}
	if !strings.Contains(text, `"task_id":"t1"`) || !strings.Contains(text, `"message":"working"`) {
		t.Fatalf("missing task_running payload: %s", text)
	}
	for _, forbidden := range []string{`"error":`, `"result":`, `"description":`} {
		if strings.Contains(taskBlock, forbidden) {
			t.Fatalf("unexpected task_running field %s: %s", forbidden, taskBlock)
		}
	}
	if strings.Contains(text, "event: values") {
		t.Fatalf("unexpected values event: %s", text)
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

func TestSkillInstallAcceptsCamelCaseThreadID(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-skill-camel"
	uploadDir := s.uploadsDir(threadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir upload dir: %v", err)
	}
	archivePath := filepath.Join(uploadDir, "camel.skill")
	if err := writeSkillArchive(archivePath, "camel-skill"); err != nil {
		t.Fatalf("write skill archive: %v", err)
	}

	body := `{"threadId":"` + threadID + `","path":"/mnt/user-data/uploads/camel.skill"}`
	resp, err := http.Post(ts.URL+"/api/skills/install", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("install request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	target := filepath.Join(s.dataRoot, "skills", "custom", "camel-skill", "SKILL.md")
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

func TestAgentsListOmitsSoul(t *testing.T) {
	_, ts := newCompatTestServer(t)

	createBody := `{"name":"soul-hidden","description":"a","model":"qwen/Qwen3.5-9B","tool_groups":["file"],"soul":"private prompt"}`
	createResp, err := http.Post(ts.URL+"/api/agents", "application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("create agent request: %v", err)
	}
	createResp.Body.Close()
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

	var payload struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode agents: %v", err)
	}
	if len(payload.Agents) == 0 {
		t.Fatal("expected agents")
	}
	for _, agent := range payload.Agents {
		if agent["name"] == "soul-hidden" {
			if _, ok := agent["soul"]; ok {
				t.Fatalf("expected soul omitted in list payload: %#v", agent)
			}
			return
		}
	}
	t.Fatalf("agent not found in payload: %#v", payload.Agents)
}

func TestUserProfileAcceptsNullContent(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.setUserProfileLocked("existing profile")

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/user-profile", strings.NewReader(`{"content":null}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put user profile: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(b))
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if value, ok := payload["content"]; !ok || value != nil {
		t.Fatalf("content=%#v", payload["content"])
	}

	if got := s.getUserProfileLocked(); got != "" {
		t.Fatalf("userProfile=%q", got)
	}
	data, err := os.ReadFile(s.userProfilePath())
	if err != nil {
		t.Fatalf("read user profile file: %v", err)
	}
	if string(data) != "" {
		t.Fatalf("user profile file=%q", string(data))
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
