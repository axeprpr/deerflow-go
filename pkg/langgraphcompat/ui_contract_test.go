package langgraphcompat

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUIContractModelsListEnvelope(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.uiStateMu.Lock()
	s.models = map[string]gatewayModel{
		"qwen3.5-27b": {
			ID:          "qwen3.5-27b",
			Name:        "qwen3.5-27b",
			Model:       "Qwen/Qwen3.5-27B",
			DisplayName: "Qwen 3.5 27B",
		},
	}
	s.uiStateMu.Unlock()

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/models", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Models) == 0 {
		t.Fatalf("models=%d body=%s", len(payload.Models), resp.Body.String())
	}
	var matched map[string]any
	for _, model := range payload.Models {
		if model["name"] == "qwen3.5-27b" {
			matched = model
			break
		}
	}
	if matched == nil {
		t.Fatalf("missing injected model in %#v", payload.Models)
	}
	for _, key := range []string{"id", "name", "model", "display_name"} {
		if _, ok := matched[key]; !ok {
			t.Fatalf("missing key %q in %#v", key, matched)
		}
	}
}

func TestUIContractSkillsListAndEnableShape(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.uiStateMu.Lock()
	s.skills = map[string]gatewaySkill{
		"demo-skill": {
			Name:        "demo-skill",
			Description: "demo",
			Category:    "custom",
			License:     "MIT",
			Enabled:     false,
		},
	}
	s.uiStateMu.Unlock()

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/skills", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listed struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Skills) == 0 {
		t.Fatalf("skills=%d body=%s", len(listed.Skills), listResp.Body.String())
	}
	foundDemo := false
	for _, skill := range listed.Skills {
		if skill["name"] == "demo-skill" {
			foundDemo = true
			break
		}
	}
	if !foundDemo {
		t.Fatalf("missing demo-skill in %#v", listed.Skills)
	}

	enableResp := performCompatRequest(t, handler, http.MethodPut, "/api/skills/demo-skill", strings.NewReader(`{"enabled":true}`), map[string]string{
		"Content-Type": "application/json",
	})
	if enableResp.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", enableResp.Code, enableResp.Body.String())
	}
	var skill map[string]any
	if err := json.Unmarshal(enableResp.Body.Bytes(), &skill); err != nil {
		t.Fatalf("decode enable: %v", err)
	}
	for _, key := range []string{"name", "description", "category", "license", "enabled"} {
		if _, ok := skill[key]; !ok {
			t.Fatalf("missing key %q in %#v", key, skill)
		}
	}
}

func TestUIContractAgentsSurface(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.uiStateMu.Lock()
	s.agents = map[string]gatewayAgent{
		"writer-bot": {
			Name:        "writer-bot",
			Description: "writer",
			ToolGroups:  []string{"file"},
		},
	}
	s.uiStateMu.Unlock()

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listed struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Agents) != 1 {
		t.Fatalf("agents=%d body=%s", len(listed.Agents), listResp.Body.String())
	}

	checkResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents/check?name=new-agent", nil, nil)
	if checkResp.Code != http.StatusOK {
		t.Fatalf("check status=%d body=%s", checkResp.Code, checkResp.Body.String())
	}
	var check map[string]any
	if err := json.Unmarshal(checkResp.Body.Bytes(), &check); err != nil {
		t.Fatalf("decode check: %v", err)
	}
	if _, ok := check["available"]; !ok {
		t.Fatalf("payload=%#v", check)
	}
	if check["name"] != "new-agent" {
		t.Fatalf("name=%#v", check["name"])
	}
}

func TestUIContractUserProfileAndMCPShape(t *testing.T) {
	_, handler := newCompatTestServer(t)

	profileResp := performCompatRequest(t, handler, http.MethodPut, "/api/user-profile", strings.NewReader(`{"content":"hello profile"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if profileResp.Code != http.StatusOK {
		t.Fatalf("profile put status=%d body=%s", profileResp.Code, profileResp.Body.String())
	}
	var profile map[string]any
	if err := json.Unmarshal(profileResp.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if profile["content"] != "hello profile" {
		t.Fatalf("payload=%#v", profile)
	}

	mcpResp := performCompatRequest(t, handler, http.MethodGet, "/api/mcp/config", nil, nil)
	if mcpResp.Code != http.StatusOK {
		t.Fatalf("mcp status=%d body=%s", mcpResp.Code, mcpResp.Body.String())
	}
	var mcp map[string]any
	if err := json.Unmarshal(mcpResp.Body.Bytes(), &mcp); err != nil {
		t.Fatalf("decode mcp: %v", err)
	}
	if _, ok := mcp["mcp_servers"]; !ok {
		t.Fatalf("payload=%#v", mcp)
	}
}

func TestUIContractMemorySurface(t *testing.T) {
	_, handler := newCompatTestServer(t)

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/memory", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	var mem map[string]any
	if err := json.Unmarshal(getResp.Body.Bytes(), &mem); err != nil {
		t.Fatalf("decode memory: %v", err)
	}
	for _, key := range []string{"version", "lastUpdated", "user", "history", "facts"} {
		if _, ok := mem[key]; !ok {
			t.Fatalf("missing key %q in %#v", key, mem)
		}
	}

	deleteResp := performCompatRequest(t, handler, http.MethodDelete, "/api/memory/facts/missing-fact", nil, nil)
	if deleteResp.Code != http.StatusNotFound {
		t.Fatalf("delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	var errPayload map[string]any
	if err := json.Unmarshal(deleteResp.Body.Bytes(), &errPayload); err != nil {
		t.Fatalf("decode delete error: %v", err)
	}
	if errPayload["detail"] != "Memory fact 'missing-fact' not found." {
		t.Fatalf("detail=%#v", errPayload["detail"])
	}
}

func TestUIContractUploadsSurface(t *testing.T) {
	_, handler := newCompatTestServer(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(part, "hello"); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-ui-uploads/uploads", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	var uploaded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	for _, key := range []string{"success", "files", "message"} {
		if _, ok := uploaded[key]; !ok {
			t.Fatalf("missing key %q in %#v", key, uploaded)
		}
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-ui-uploads/uploads/list", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, key := range []string{"files", "count"} {
		if _, ok := listed[key]; !ok {
			t.Fatalf("missing key %q in %#v", key, listed)
		}
	}
}

func TestUIContractLangGraphStrictRunValidation(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := "550e8400-e29b-41d4-a716-446655440000"

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/langgraph/threads/"+threadID+"/runs/stream", strings.NewReader(`{"input":{"messages":[{"role":"user","content":"hi"}]}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["detail"] != "assistant_id required" {
		t.Fatalf("detail=%#v", payload["detail"])
	}

	invalidResp := performCompatRequest(t, handler, http.MethodPost, "/api/langgraph/threads/not-a-uuid/runs/stream", strings.NewReader(`{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if invalidResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", invalidResp.Code, invalidResp.Body.String())
	}
	var invalid map[string]any
	if err := json.Unmarshal(invalidResp.Body.Bytes(), &invalid); err != nil {
		t.Fatalf("decode invalid response: %v", err)
	}
	if invalid["detail"] != "invalid thread_id" {
		t.Fatalf("detail=%#v", invalid["detail"])
	}
}
