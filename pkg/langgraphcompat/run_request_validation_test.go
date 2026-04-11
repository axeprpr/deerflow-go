package langgraphcompat

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestStrictLangGraphRunsStreamRequiresAssistantID(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := uuid.NewString()

	resp := performCompatRequest(
		t,
		handler,
		http.MethodPost,
		"/api/langgraph/threads/"+threadID+"/runs/stream",
		strings.NewReader(`{"input":{"messages":[{"role":"user","content":"hi"}]}}`),
		map[string]string{"Content-Type": "application/json"},
	)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "assistant_id required") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestStrictLangGraphRunsStreamRequiresUUIDThreadID(t *testing.T) {
	_, handler := newCompatTestServer(t)

	resp := performCompatRequest(
		t,
		handler,
		http.MethodPost,
		"/api/langgraph/threads/thread-not-uuid/runs/stream",
		strings.NewReader(`{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]}}`),
		map[string]string{"Content-Type": "application/json"},
	)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}

func TestCompatRunsStreamStillAcceptsLegacyThreadID(t *testing.T) {
	s, handler := newCompatTestServer(t)
	s.llmProvider = fakeLLMProvider{}

	resp := performCompatRequest(
		t,
		handler,
		http.MethodPost,
		"/threads/thread-legacy/runs/stream",
		strings.NewReader(`{"assistant_id":"lead_agent","input":{"messages":[{"role":"user","content":"hi"}]}}`),
		map[string]string{"Content-Type": "application/json"},
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestStrictLangGraphRunsCreateRequiresAssistantID(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := uuid.NewString()

	resp := performCompatRequest(
		t,
		handler,
		http.MethodPost,
		"/api/langgraph/threads/"+threadID+"/runs",
		strings.NewReader(`{"input":{"messages":[{"role":"user","content":"hi"}]}}`),
		map[string]string{"Content-Type": "application/json"},
	)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "assistant_id required") {
		t.Fatalf("body=%q", resp.Body.String())
	}
}
