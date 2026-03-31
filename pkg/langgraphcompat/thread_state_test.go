package langgraphcompat

import (
	"net/http"
	"strings"
	"testing"
)

func TestThreadStatePatchUpdatesTitleFromValues(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-rename", map[string]any{"title": "Old title"})

	resp := performCompatRequest(t, handler, http.MethodPatch, "/threads/thread-rename/state", strings.NewReader(`{"values":{"title":"New title"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	state := s.getThreadState("thread-rename")
	if state == nil {
		t.Fatal("state is nil")
	}
	if got := asString(state.Values["title"]); got != "New title" {
		t.Fatalf("title=%q want=New title", got)
	}
}

func TestThreadStatePostMergesValuesAndMetadata(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.ensureSession("thread-state-post", map[string]any{"title": "Old title", "agent_type": "writer"})

	resp := performCompatRequest(t, handler, http.MethodPost, "/threads/thread-state-post/state", strings.NewReader(`{"values":{"title":"Updated title"},"metadata":{"agent_type":"coder"}}`), map[string]string{
		"Content-Type": "application/json",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}

	s.sessionsMu.RLock()
	session := s.sessions["thread-state-post"]
	s.sessionsMu.RUnlock()
	if session == nil {
		t.Fatal("session is nil")
	}
	if got := asString(session.Metadata["title"]); got != "Updated title" {
		t.Fatalf("title=%q want=Updated title", got)
	}
	if got := asString(session.Metadata["agent_type"]); got != "coder" {
		t.Fatalf("agent_type=%q want=coder", got)
	}
}

func TestThreadStateIncludesThreadDataAndConfigurableContext(t *testing.T) {
	s, _ := newCompatTestServer(t)
	session := s.ensureSession("thread-context", map[string]any{"title": "Context thread", "agent_type": "coder"})
	session.Configurable["model_name"] = "gpt-5"
	session.Configurable["is_plan_mode"] = true
	session.Configurable["reasoning_effort"] = "high"

	state := s.getThreadState("thread-context")
	if state == nil {
		t.Fatal("state is nil")
	}

	threadData, ok := state.Values["thread_data"].(map[string]any)
	if !ok {
		t.Fatalf("thread_data=%#v", state.Values["thread_data"])
	}
	if got := asString(threadData["workspace_path"]); !strings.Contains(got, "/threads/thread-context/user-data/workspace") {
		t.Fatalf("workspace_path=%q", got)
	}

	config, ok := state.Config["configurable"].(map[string]any)
	if !ok {
		t.Fatalf("config=%#v", state.Config)
	}
	if got := asString(config["model_name"]); got != "gpt-5" {
		t.Fatalf("model_name=%q want=gpt-5", got)
	}
	if got, _ := config["is_plan_mode"].(bool); !got {
		t.Fatalf("is_plan_mode=%v want=true", config["is_plan_mode"])
	}
	if got := asString(config["reasoning_effort"]); got != "high" {
		t.Fatalf("reasoning_effort=%q want=high", got)
	}
}
