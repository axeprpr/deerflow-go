package langgraphcompat

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadGatewayCompatFilesLoadsAgentsAndUserProfileFromDisk(t *testing.T) {
	s, handler := newCompatTestServer(t)

	agentDir := s.agentDir("writer-bot")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.yaml"), []byte("description: Draft long-form content.\nmodel: gpt-5\ntool_groups:\n  - builtin\n  - file\n"), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("# Writer Bot\n\nStay concise."), 0o644); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	if err := os.WriteFile(s.userProfilePath(), []byte("Prefers concise answers.\n"), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}

	if err := s.loadGatewayCompatFiles(); err != nil {
		t.Fatalf("loadGatewayCompatFiles: %v", err)
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list agents status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"name":"writer-bot"`) {
		t.Fatalf("list agents body=%s", listResp.Body.String())
	}

	getResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents/writer-bot", nil, nil)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get agent status=%d body=%s", getResp.Code, getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), "Stay concise.") {
		t.Fatalf("get agent body=%s", getResp.Body.String())
	}

	profileResp := performCompatRequest(t, handler, http.MethodGet, "/api/user-profile", nil, nil)
	if profileResp.Code != http.StatusOK {
		t.Fatalf("get user profile status=%d body=%s", profileResp.Code, profileResp.Body.String())
	}
	if !strings.Contains(profileResp.Body.String(), "Prefers concise answers.") {
		t.Fatalf("user profile body=%s", profileResp.Body.String())
	}
}

func TestUserProfilePutPersistsUSERMD(t *testing.T) {
	s, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPut, "/api/user-profile", strings.NewReader(`{"content":"Prefers direct answers."}`), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusOK {
		t.Fatalf("put user profile status=%d body=%s", resp.Code, resp.Body.String())
	}

	data, err := os.ReadFile(s.userProfilePath())
	if err != nil {
		t.Fatalf("read USER.md: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "Prefers direct answers." {
		t.Fatalf("USER.md=%q want=%q", got, "Prefers direct answers.")
	}
}
