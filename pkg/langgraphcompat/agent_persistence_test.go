package langgraphcompat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/memory"
)

func TestAgentCreateUpdateDeleteSyncsFiles(t *testing.T) {
	s, handler := newCompatTestServer(t)

	createBody := `{
		"name":"writer-bot",
		"description":"Draft long-form content.",
		"model":"gpt-5",
		"tool_groups":["builtin","file"],
		"soul":"# Writer Bot\n\nStay concise."
	}`
	createResp := performCompatRequest(t, handler, http.MethodPost, "/api/agents", strings.NewReader(createBody), map[string]string{"Content-Type": "application/json"})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	agentDir := s.agentDir("writer-bot")
	assertFileContains(t, filepath.Join(agentDir, "SOUL.md"), "Stay concise.")
	assertFileContains(t, filepath.Join(agentDir, "config.yaml"), "description: Draft long-form content.")
	assertFileContains(t, filepath.Join(agentDir, "config.yaml"), "- builtin")
	assertFileContains(t, filepath.Join(agentDir, "config.yaml"), "- file")

	agentJSON, err := os.ReadFile(filepath.Join(agentDir, "agent.json"))
	if err != nil {
		t.Fatalf("read agent.json: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(agentJSON, &payload); err != nil {
		t.Fatalf("decode agent.json: %v", err)
	}
	if payload["name"] != "writer-bot" {
		t.Fatalf("agent.json name=%v want writer-bot", payload["name"])
	}

	updateBody := `{
		"description":"Draft polished articles.",
		"tool_groups":["builtin"],
		"soul":"# Writer Bot\n\nPrefer clear structure."
	}`
	updateResp := performCompatRequest(t, handler, http.MethodPut, "/api/agents/writer-bot", strings.NewReader(updateBody), map[string]string{"Content-Type": "application/json"})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}

	assertFileContains(t, filepath.Join(agentDir, "SOUL.md"), "Prefer clear structure.")
	assertFileContains(t, filepath.Join(agentDir, "config.yaml"), "description: Draft polished articles.")
	configData, err := os.ReadFile(filepath.Join(agentDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if strings.Contains(string(configData), "- file") {
		t.Fatalf("config.yaml still contains removed tool group: %s", string(configData))
	}

	deleteResp := performCompatRequest(t, handler, http.MethodDelete, "/api/agents/writer-bot", nil, nil)
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	if _, err := os.Stat(agentDir); !os.IsNotExist(err) {
		t.Fatalf("agent directory still exists after delete: err=%v", err)
	}
}

func TestAgentDeleteClearsScopedMemory(t *testing.T) {
	s, handler := newCompatTestServer(t)

	store, err := memory.NewFileStore(filepath.Join(s.dataRoot, "memory"))
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	if err := store.AutoMigrate(context.Background()); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	s.memoryRuntime = harness.NewMemoryRuntime(store, nil)

	createResp := performCompatRequest(t, handler, http.MethodPost, "/api/agents", strings.NewReader(`{
		"name":"writer-bot",
		"description":"Draft long-form content.",
		"soul":"# Writer Bot\n\nStay concise."
	}`), map[string]string{"Content-Type": "application/json"})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	sessionID := deriveMemorySessionID("", "writer-bot")
	if err := store.Save(context.Background(), memory.Document{
		SessionID: sessionID,
		Source:    sessionID,
		User:      memory.UserMemory{TopOfMind: "Remember the editorial tone."},
	}); err != nil {
		t.Fatalf("seed scoped memory: %v", err)
	}

	deleteResp := performCompatRequest(t, handler, http.MethodDelete, "/api/agents/writer-bot", nil, nil)
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}

	if _, err := store.Load(context.Background(), sessionID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("load deleted scoped memory err=%v want ErrNotFound", err)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s missing %q in %q", path, want, string(data))
	}
}
