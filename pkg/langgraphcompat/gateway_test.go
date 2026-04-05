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
		strings.NewReader(`{"threadId":"thread-create-top-level","title":"Created Top","viewedImages":{"/tmp/chart.png":{"base64":"xyz","mime_type":"image/png"}}}`),
	)
	if err != nil {
		t.Fatalf("post thread: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(body))
	}

	state := s.getThreadState("thread-create-top-level")
	if state == nil {
		t.Fatal("state missing")
	}
	if got := state.Values["title"]; got != "Created Top" {
		t.Fatalf("title=%v want Created Top", got)
	}
	viewedImages, ok := state.Values["viewed_images"].(map[string]any)
	if !ok || len(viewedImages) != 1 {
		t.Fatalf("viewed_images=%#v", state.Values["viewed_images"])
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

func TestThreadRunsCreateAcceptsTopLevelMessages(t *testing.T) {
	s, ts := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}
	body := `{"assistant_id":"lead_agent","messages":[{"role":"user","content":"hi from top level"}]}`
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

func TestThreadStatePostAcceptsMetadata(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-state-post-metadata"
	s.ensureSession(threadID, nil)

	body := `{"metadata":{"assistant_id":"lead_agent","graph_id":"lead_agent"},"values":{"title":"Updated"}}`
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
	if state.Values["title"] != "Updated" {
		t.Fatalf("title=%v", state.Values["title"])
	}
}

func TestThreadStatePatchAcceptsValuesPayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-patch-values"
	s.ensureSession(threadID, map[string]any{"title": "Before"})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID+"/state",
		strings.NewReader(`{"values":{"title":"After"}}`),
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
	if got := state.Values["title"]; got != "After" {
		t.Fatalf("title=%v want After", got)
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

func TestThreadUpdateAcceptsValuesPayload(t *testing.T) {
	s, ts := newCompatTestServer(t)
	threadID := "thread-update-values"
	s.ensureSession(threadID, map[string]any{"title": "Before"})

	req, _ := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/threads/"+threadID,
		strings.NewReader(`{"values":{"title":"After","todos":[{"content":"ship sqlite","status":"completed"}]}}`),
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
	if got := state.Values["title"]; got != "After" {
		t.Fatalf("title=%v want After", got)
	}
	todos, ok := state.Values["todos"].([]map[string]any)
	if !ok || len(todos) != 1 {
		t.Fatalf("todos=%#v", state.Values["todos"])
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
	body := `{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]},"config":{"configurable":{"modelName":"deepseek/deepseek-r1","thinkingEnabled":false,"isPlanMode":true,"subagentEnabled":true,"reasoningEffort":"high","agentType":"deep_research","maxTokens":321}}}`
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
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
	}
	if !strings.Contains(text, "event: end") {
		t.Fatalf("missing end event: %s", text)
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
	if !strings.Contains(text, "event: task_started") {
		t.Fatalf("missing task_started event: %s", text)
	}
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
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
	if !strings.Contains(text, "event: task_started") {
		t.Fatalf("missing task_started event: %s", text)
	}
	if !strings.Contains(text, "event: messages-tuple") {
		t.Fatalf("missing messages-tuple event: %s", text)
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
