package langgraphcompat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveGatewayExtensionsConfigPathFallsBackToLegacyMCPConfig(t *testing.T) {
	projectRoot := t.TempDir()
	legacyPath := filepath.Join(projectRoot, "mcp_config.json")
	if err := os.WriteFile(legacyPath, []byte(`{"mcpServers":{},"skills":{}}`), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	if got := resolveGatewayExtensionsConfigPath(); got != legacyPath {
		t.Fatalf("path=%q want=%q", got, legacyPath)
	}
}

func TestLoadGatewayExtensionsConfigResolvesEnvPlaceholders(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, "extensions_config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "docs": {
      "enabled": true,
      "type": "http",
      "url": "$DOCS_URL",
      "headers": {
        "Authorization": "$DOCS_TOKEN"
      },
      "env": {
        "API_KEY": "$DOCS_TOKEN",
        "STATIC_VALUE": "keep-me"
      },
      "description": "Docs server"
    }
  },
  "skills": {
    "deep-research": {
      "enabled": false
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DOCS_URL", "https://docs.example.com/mcp")
	t.Setenv("DOCS_TOKEN", "secret-token")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	s, _ := newCompatTestServer(t)
	if err := s.loadGatewayExtensionsConfig(); err != nil {
		t.Fatalf("loadGatewayExtensionsConfig: %v", err)
	}

	s.uiStateMu.RLock()
	server := s.mcpConfig.MCPServers["docs"]
	skill := s.skills[skillStorageKey(skillCategoryPublic, "deep-research")]
	s.uiStateMu.RUnlock()

	if got := server.URL; got != "https://docs.example.com/mcp" {
		t.Fatalf("url=%q want=%q", got, "https://docs.example.com/mcp")
	}
	if got := server.Headers["Authorization"]; got != "secret-token" {
		t.Fatalf("authorization=%q want=%q", got, "secret-token")
	}
	if got := server.Env["API_KEY"]; got != "secret-token" {
		t.Fatalf("env API_KEY=%q want=%q", got, "secret-token")
	}
	if got := server.Env["STATIC_VALUE"]; got != "keep-me" {
		t.Fatalf("env STATIC_VALUE=%q want=%q", got, "keep-me")
	}
	if skill.Enabled {
		t.Fatal("expected deep-research skill to be disabled from extensions config")
	}
}

func TestLoadGatewayExtensionsConfigReplacesMissingEnvPlaceholdersWithEmptyString(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, "extensions_config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "docs": {
      "enabled": true,
      "type": "http",
      "url": "$MISSING_URL",
      "headers": {
        "Authorization": "$MISSING_TOKEN"
      },
      "description": "Docs server"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	s, _ := newCompatTestServer(t)
	if err := s.loadGatewayExtensionsConfig(); err != nil {
		t.Fatalf("loadGatewayExtensionsConfig: %v", err)
	}

	s.uiStateMu.RLock()
	server := s.mcpConfig.MCPServers["docs"]
	s.uiStateMu.RUnlock()

	if got := server.URL; got != "" {
		t.Fatalf("url=%q want empty", got)
	}
	if got := server.Headers["Authorization"]; got != "" {
		t.Fatalf("authorization=%q want empty", got)
	}
}

func TestLoadGatewayExtensionsConfigResolvesBracedAndDefaultEnvPlaceholders(t *testing.T) {
	projectRoot := t.TempDir()
	configPath := filepath.Join(projectRoot, "extensions_config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "docs": {
      "enabled": true,
      "type": "http",
      "url": "https://${DOCS_HOST}/mcp",
      "headers": {
        "Authorization": "Bearer ${DOCS_TOKEN:-fallback-token}"
      },
      "env": {
        "REGION": "${DOCS_REGION:-cn}",
        "HOST": "${DOCS_HOST}"
      },
      "description": "Docs server"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DOCS_HOST", "docs.example.com")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	s, _ := newCompatTestServer(t)
	if err := s.loadGatewayExtensionsConfig(); err != nil {
		t.Fatalf("loadGatewayExtensionsConfig: %v", err)
	}

	s.uiStateMu.RLock()
	server := s.mcpConfig.MCPServers["docs"]
	s.uiStateMu.RUnlock()

	if got := server.URL; got != "https://docs.example.com/mcp" {
		t.Fatalf("url=%q want=https://docs.example.com/mcp", got)
	}
	if got := server.Headers["Authorization"]; got != "Bearer fallback-token" {
		t.Fatalf("authorization=%q want=Bearer fallback-token", got)
	}
	if got := server.Env["REGION"]; got != "cn" {
		t.Fatalf("env REGION=%q want=cn", got)
	}
	if got := server.Env["HOST"]; got != "docs.example.com" {
		t.Fatalf("env HOST=%q want=docs.example.com", got)
	}
}
