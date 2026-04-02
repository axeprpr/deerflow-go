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
	if _, err := os.Stat(filepath.Join(s.dataRoot, "USER.md")); !os.IsNotExist(err) {
		t.Fatalf("legacy data root unexpectedly contains USER.md, err=%v", err)
	}
}

func TestLoadGatewayCompatFilesFallsBackToLegacyDataRoot(t *testing.T) {
	s, handler := newCompatTestServer(t)

	legacyAgentDir := filepath.Join(s.dataRoot, "agents", "legacy-bot")
	if err := os.MkdirAll(legacyAgentDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyAgentDir, "config.yaml"), []byte("description: Legacy agent.\n"), 0o644); err != nil {
		t.Fatalf("write legacy config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.dataRoot, "USER.md"), []byte("Legacy profile.\n"), 0o644); err != nil {
		t.Fatalf("write legacy USER.md: %v", err)
	}

	if err := s.loadGatewayCompatFiles(); err != nil {
		t.Fatalf("loadGatewayCompatFiles: %v", err)
	}

	agentResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents/legacy-bot", nil, nil)
	if agentResp.Code != http.StatusOK {
		t.Fatalf("get legacy agent status=%d body=%s", agentResp.Code, agentResp.Body.String())
	}

	profileResp := performCompatRequest(t, handler, http.MethodGet, "/api/user-profile", nil, nil)
	if profileResp.Code != http.StatusOK {
		t.Fatalf("get legacy user profile status=%d body=%s", profileResp.Code, profileResp.Body.String())
	}
	if !strings.Contains(profileResp.Body.String(), "Legacy profile.") {
		t.Fatalf("legacy user profile body=%s", profileResp.Body.String())
	}
}

func TestAgentCreatePersistsToCompatRootAgentsDir(t *testing.T) {
	s, handler := newCompatTestServer(t)

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/agents", strings.NewReader(`{"name":"writer-bot","description":"Writes drafts","soul":"# Writer\n\nBe concise."}`), map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusCreated {
		t.Fatalf("create agent status=%d body=%s", resp.Code, resp.Body.String())
	}

	if _, err := os.Stat(filepath.Join(s.compatRoot(), "agents", "writer-bot", "SOUL.md")); err != nil {
		t.Fatalf("stat compat root SOUL.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dataRoot, "agents", "writer-bot", "SOUL.md")); !os.IsNotExist(err) {
		t.Fatalf("legacy data root unexpectedly contains created agent, err=%v", err)
	}
}

func TestLoadGatewayCompatFilesClearsMissingDiskState(t *testing.T) {
	s, _ := newCompatTestServer(t)

	s.uiStateMu.Lock()
	s.setAgentsLocked(map[string]gatewayAgent{
		"ghost-bot": {
			Name:        "ghost-bot",
			Description: "Only in memory.",
		},
	})
	s.setUserProfileLocked("stale profile")
	s.uiStateMu.Unlock()

	if err := s.loadGatewayCompatFiles(); err != nil {
		t.Fatalf("loadGatewayCompatFiles: %v", err)
	}

	s.uiStateMu.RLock()
	defer s.uiStateMu.RUnlock()
	if agents := s.getAgentsLocked(); len(agents) != 0 {
		t.Fatalf("agents=%#v want empty", agents)
	}
	if profile := s.getUserProfileLocked(); profile != "" {
		t.Fatalf("profile=%q want empty", profile)
	}
}

func TestRefreshGatewayCompatFilesClearsRemovedCompatState(t *testing.T) {
	s, handler := newCompatTestServer(t)

	agentDir := s.agentDir("writer-bot")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.yaml"), []byte("description: Draft long-form content.\n"), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(s.userProfilePath(), []byte("Prefers concise answers.\n"), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}
	if err := s.loadGatewayCompatFiles(); err != nil {
		t.Fatalf("loadGatewayCompatFiles: %v", err)
	}

	if err := os.RemoveAll(agentDir); err != nil {
		t.Fatalf("remove agent dir: %v", err)
	}
	if err := os.Remove(s.userProfilePath()); err != nil {
		t.Fatalf("remove USER.md: %v", err)
	}

	listResp := performCompatRequest(t, handler, http.MethodGet, "/api/agents", nil, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list agents status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if strings.Contains(listResp.Body.String(), `"name":"writer-bot"`) {
		t.Fatalf("stale agent still returned: %s", listResp.Body.String())
	}

	profileResp := performCompatRequest(t, handler, http.MethodGet, "/api/user-profile", nil, nil)
	if profileResp.Code != http.StatusOK {
		t.Fatalf("get user profile status=%d body=%s", profileResp.Code, profileResp.Body.String())
	}
	if strings.Contains(profileResp.Body.String(), "Prefers concise answers.") {
		t.Fatalf("stale profile still returned: %s", profileResp.Body.String())
	}
}

func TestCompatRootDefaultsToModuleRoot(t *testing.T) {
	t.Setenv(compatRootEnv, "")
	s := &Server{dataRoot: t.TempDir()}

	root := s.compatRoot()
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("compat root %q missing go.mod: %v", root, err)
	}
}
