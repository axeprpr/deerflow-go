package langgraphcompat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
)

func TestParseRunConfigAgentType(t *testing.T) {
	cfg := parseRunConfig(map[string]any{
		"configurable": map[string]any{
			"agent_type": "researcher",
		},
	})
	if cfg.AgentType != agent.AgentTypeResearch {
		t.Fatalf("AgentType = %q, want %q", cfg.AgentType, agent.AgentTypeResearch)
	}
}

func TestThreadClarificationHandlers(t *testing.T) {
	manager := clarification.NewManager(4)
	server := &Server{
		clarify:    manager,
		clarifyAPI: clarification.NewAPI(manager),
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
	}
	server.ensureSession("thread-1", nil)

	createBody, _ := json.Marshal(map[string]any{
		"question": "Which mode?",
		"options": []map[string]any{
			{"label": "Fast", "value": "fast"},
			{"label": "Safe", "value": "safe"},
		},
		"required": true,
	})
	createReq := httptest.NewRequest(http.MethodPost, "/threads/thread-1/clarifications", bytes.NewReader(createBody))
	createReq.SetPathValue("thread_id", "thread-1")
	createRes := httptest.NewRecorder()
	server.handleThreadClarificationCreate(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createRes.Code, http.StatusCreated)
	}

	var created clarification.Clarification
	if err := json.Unmarshal(createRes.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create response missing clarification id")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/threads/thread-1/clarifications/"+created.ID, nil)
	getReq.SetPathValue("thread_id", "thread-1")
	getReq.SetPathValue("id", created.ID)
	getRes := httptest.NewRecorder()
	server.handleThreadClarificationGet(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getRes.Code, http.StatusOK)
	}

	resolveBody, _ := json.Marshal(map[string]any{"answer": "safe"})
	resolveReq := httptest.NewRequest(http.MethodPost, "/threads/thread-1/clarifications/"+created.ID+"/resolve", bytes.NewReader(resolveBody))
	resolveReq.SetPathValue("thread_id", "thread-1")
	resolveReq.SetPathValue("id", created.ID)
	resolveRes := httptest.NewRecorder()
	server.handleThreadClarificationResolve(resolveRes, resolveReq)
	if resolveRes.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want %d", resolveRes.Code, http.StatusOK)
	}

	var resolved clarification.Clarification
	if err := json.Unmarshal(resolveRes.Body.Bytes(), &resolved); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	if resolved.Answer != "safe" {
		t.Fatalf("resolved answer = %q, want safe", resolved.Answer)
	}
}

func TestThreadClarificationListHandler(t *testing.T) {
	manager := clarification.NewManager(4)
	server := &Server{
		clarify:    manager,
		clarifyAPI: clarification.NewAPI(manager),
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
	}
	server.ensureSession("thread-1", nil)

	first, err := manager.Request(clarification.WithThreadID(context.Background(), "thread-1"), clarification.ClarificationRequest{
		Question: "First?",
	})
	if err != nil {
		t.Fatalf("first request error = %v", err)
	}
	second, err := manager.Request(clarification.WithThreadID(context.Background(), "thread-1"), clarification.ClarificationRequest{
		Question: "Second?",
	})
	if err != nil {
		t.Fatalf("second request error = %v", err)
	}
	if _, err := manager.Request(clarification.WithThreadID(context.Background(), "thread-2"), clarification.ClarificationRequest{
		Question: "Other thread?",
	}); err != nil {
		t.Fatalf("other request error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/threads/thread-1/clarifications", nil)
	req.SetPathValue("thread_id", "thread-1")
	res := httptest.NewRecorder()
	server.handleThreadClarificationList(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", res.Code, http.StatusOK)
	}

	var payload struct {
		Clarifications []clarification.Clarification `json:"clarifications"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(payload.Clarifications) != 2 {
		t.Fatalf("clarifications len = %d, want 2", len(payload.Clarifications))
	}
	if payload.Clarifications[0].ID != first.ID || payload.Clarifications[1].ID != second.ID {
		t.Fatalf("clarification ids = [%s %s], want [%s %s]", payload.Clarifications[0].ID, payload.Clarifications[1].ID, first.ID, second.ID)
	}
}
