package langgraphcompat

import (
	"bytes"
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

func TestParseRunConfigMode(t *testing.T) {
	cfg := parseRunConfig(map[string]any{
		"configurable": map[string]any{
			"mode": "pro",
		},
	})
	if cfg.Mode != "pro" {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, "pro")
	}
}

func TestParseRunConfigRecursionLimit(t *testing.T) {
	cfg := parseRunConfig(map[string]any{
		"recursion_limit": 100,
	})
	if cfg.MaxTurns == nil || *cfg.MaxTurns != 100 {
		t.Fatalf("MaxTurns = %v, want 100", cfg.MaxTurns)
	}
}

func TestParseRunConfigMemoryScopeHints(t *testing.T) {
	cfg := parseRunConfig(map[string]any{
		"configurable": map[string]any{
			"memoryUserId":    "user-1",
			"memory_group_id": "group-1",
			"memoryNamespace": "workspace-a",
		},
	})
	if cfg.MemoryUserID != "user-1" {
		t.Fatalf("MemoryUserID = %q", cfg.MemoryUserID)
	}
	if cfg.MemoryGroupID != "group-1" {
		t.Fatalf("MemoryGroupID = %q", cfg.MemoryGroupID)
	}
	if cfg.MemoryNamespace != "workspace-a" {
		t.Fatalf("MemoryNamespace = %q", cfg.MemoryNamespace)
	}
}

func TestThreadClarificationHandlers(t *testing.T) {
	manager := clarification.NewManager(4)
	root := t.TempDir()
	server := &Server{
		clarify:    manager,
		clarifyAPI: clarification.NewAPI(manager),
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
		dataRoot:   root,
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

func TestThreadClarificationCreateAcceptsClarificationTypeAlias(t *testing.T) {
	manager := clarification.NewManager(4)
	root := t.TempDir()
	server := &Server{
		clarify:    manager,
		clarifyAPI: clarification.NewAPI(manager),
		sessions:   make(map[string]*Session),
		runs:       make(map[string]*Run),
		dataRoot:   root,
	}
	server.ensureSession("thread-2", nil)

	createBody, _ := json.Marshal(map[string]any{
		"clarification_type": "approach_choice",
		"question":           "Which mode?",
		"context":            "There are multiple valid modes.",
		"options":            []string{"Fast", "Safe"},
		"required":           true,
	})
	createReq := httptest.NewRequest(http.MethodPost, "/threads/thread-2/clarifications", bytes.NewReader(createBody))
	createReq.SetPathValue("thread_id", "thread-2")
	createRes := httptest.NewRecorder()
	server.handleThreadClarificationCreate(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createRes.Code, http.StatusCreated)
	}

	var created clarification.Clarification
	if err := json.Unmarshal(createRes.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Type != "choice" {
		t.Fatalf("created type = %q, want choice", created.Type)
	}
	if created.ClarificationType != "approach_choice" {
		t.Fatalf("created clarification_type = %q, want approach_choice", created.ClarificationType)
	}
	if created.Context != "There are multiple valid modes." {
		t.Fatalf("created context = %q", created.Context)
	}
}
