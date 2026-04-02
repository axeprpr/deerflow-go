package langgraphcompat

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestThreadCreateAppliesValuesConfigAndStatus(t *testing.T) {
	_, handler := newCompatTestServer(t)

	body := `{
		"thread_id":"thread-create-configured",
		"metadata":{"agent_name":"writer-bot"},
		"values":{"title":"Quarterly review","sidebar_tab":"artifacts"},
		"status":"busy",
		"config":{"configurable":{"model_name":"openai/gpt-5","agent_name":"writer-bot"}}
	}`
	resp := performCompatRequest(t, handler, http.MethodPost, "/threads", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	var thread map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &thread); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := asString(thread["status"]); got != "busy" {
		t.Fatalf("status=%q want=busy", got)
	}

	values, ok := thread["values"].(map[string]any)
	if !ok {
		t.Fatalf("values=%#v", thread["values"])
	}
	if got := asString(values["title"]); got != "Quarterly review" {
		t.Fatalf("title=%q want=Quarterly review", got)
	}
	if got := asString(values["sidebar_tab"]); got != "artifacts" {
		t.Fatalf("sidebar_tab=%q want=artifacts", got)
	}
	if got := asString(values["agent_name"]); got != "writer-bot" {
		t.Fatalf("agent_name=%q want=writer-bot", got)
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

func TestThreadUpdateAppliesValuesConfigAndStatus(t *testing.T) {
	s, handler := newCompatTestServer(t)
	session := s.ensureSession("thread-update-configured", map[string]any{"title": "Old title"})
	session.Status = "idle"

	body := `{
		"metadata":{"agent_type":"coder"},
		"values":{"title":"New title","draft_id":"draft-42"},
		"status":"running",
		"config":{"configurable":{"thinking_enabled":false}}
	}`
	resp := performCompatRequest(t, handler, http.MethodPatch, "/threads/thread-update-configured", strings.NewReader(body), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	reloaded := s.getThreadState("thread-update-configured")
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
	updated := s.sessions["thread-update-configured"]
	s.sessionsMu.RUnlock()
	if updated == nil {
		t.Fatal("expected stored session")
	}
	if got := updated.Status; got != "running" {
		t.Fatalf("status=%q want=running", got)
	}
	if got := asString(updated.Metadata["agent_type"]); got != "coder" {
		t.Fatalf("agent_type=%q want=coder", got)
	}
	if got, ok := updated.Configurable["thinking_enabled"].(bool); !ok || got {
		t.Fatalf("thinking_enabled=%#v want false", updated.Configurable["thinking_enabled"])
	}
}
