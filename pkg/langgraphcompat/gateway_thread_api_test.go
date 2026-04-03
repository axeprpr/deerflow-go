package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestGatewayThreadGetReturnsThreadEnvelope(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-gateway-get", map[string]any{"agent_name": "writer-bot"})
	applyThreadStateUpdate(session, map[string]any{
		"title":       "Release checklist",
		"sidebar_tab": "artifacts",
	}, map[string]any{
		"agent_type": "coder",
	})
	session.Configurable["model_name"] = "openai/gpt-5"

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/thread-gateway-get", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var thread map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &thread); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := asString(thread["thread_id"]); got != "thread-gateway-get" {
		t.Fatalf("thread_id=%q want=thread-gateway-get", got)
	}
	if got := asString(thread["status"]); got != "idle" {
		t.Fatalf("status=%q want=idle", got)
	}

	values, ok := thread["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", thread["values"])
	}
	if got := asString(values["title"]); got != "Release checklist" {
		t.Fatalf("title=%q want=Release checklist", got)
	}
	if got := asString(values["sidebar_tab"]); got != "artifacts" {
		t.Fatalf("sidebar_tab=%q want=artifacts", got)
	}

	config, ok := thread["config"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", thread["config"])
	}
	configurable, ok := config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("configurable=%#v", config["configurable"])
	}
	if got := asString(configurable["model_name"]); got != "openai/gpt-5" {
		t.Fatalf("model_name=%q want=openai/gpt-5", got)
	}
}

func TestGatewayThreadPatchUpdatesThreadEnvelope(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-gateway-patch", map[string]any{"title": "Old title"})
	session.Status = "idle"

	body := `{
		"metadata":{"agent_name":"writer-bot"},
		"values":{"title":"New title","draft_id":"draft-42"},
		"status":"busy",
		"config":{"configurable":{"model_name":"openai/gpt-5"}}
	}`
	resp := performCompatRequest(t, handler, http.MethodPatch, "/api/threads/thread-gateway-patch", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	reloaded := s.getThreadState("thread-gateway-patch")
	if reloaded == nil {
		t.Fatal("expected thread state")
	}
	if got := asString(reloaded.Values["title"]); got != "New title" {
		t.Fatalf("title=%q want=New title", got)
	}
	if got := asString(reloaded.Values["draft_id"]); got != "draft-42" {
		t.Fatalf("draft_id=%q want=draft-42", got)
	}

	s.sessionsMu.RLock()
	updated := s.sessions["thread-gateway-patch"]
	s.sessionsMu.RUnlock()
	if updated == nil {
		t.Fatal("expected stored session")
	}
	if got := updated.Status; got != "busy" {
		t.Fatalf("status=%q want=busy", got)
	}
	if got := asString(updated.Metadata["agent_name"]); got != "writer-bot" {
		t.Fatalf("agent_name=%q want=writer-bot", got)
	}
	if got := asString(updated.Configurable["model_name"]); got != "openai/gpt-5" {
		t.Fatalf("model_name=%q want=openai/gpt-5", got)
	}
}

func TestGatewayThreadGetRejectsInvalidThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/bad.id", nil, nil)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestGatewayThreadPatchRejectsInvalidThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPatch, "/api/threads/bad.id", strings.NewReader(`{"values":{"title":"ignored"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}
