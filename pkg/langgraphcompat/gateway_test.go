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
