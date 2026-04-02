package guardrails

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowlistProviderAllowsWhenNoRestrictions(t *testing.T) {
	provider := NewAllowlistProvider(nil, nil)
	decision, err := provider.Evaluate(Request{ToolName: "bash"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !decision.Allow {
		t.Fatalf("decision.Allow = false want true")
	}
}

func TestAllowlistProviderDeniesUnlistedTool(t *testing.T) {
	provider := NewAllowlistProvider([]string{"web_search"}, nil)
	decision, err := provider.Evaluate(Request{ToolName: "bash"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Allow {
		t.Fatal("decision.Allow = true want false")
	}
	if len(decision.Reasons) == 0 || decision.Reasons[0].Code != "oap.tool_not_allowed" {
		t.Fatalf("reasons = %#v", decision.Reasons)
	}
}

func TestAllowlistProviderDeniesDeniedTool(t *testing.T) {
	provider := NewAllowlistProvider([]string{"bash", "web_search"}, []string{"bash"})
	decision, err := provider.Evaluate(Request{ToolName: "bash"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Allow {
		t.Fatal("decision.Allow = true want false")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("DEERFLOW_GUARDRAILS_ENABLED", "true")
	t.Setenv("DEERFLOW_GUARDRAILS_ALLOWED_TOOLS", "web_search, read_file")
	t.Setenv("DEERFLOW_GUARDRAILS_DENIED_TOOLS", "bash")
	t.Setenv("DEERFLOW_GUARDRAILS_FAIL_CLOSED", "false")
	t.Setenv("DEERFLOW_GUARDRAILS_PASSPORT", "./guardrails/passport.json")

	cfg := LoadConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("Enabled = false want true")
	}
	if cfg.FailClosed {
		t.Fatal("FailClosed = true want false")
	}
	if got := len(cfg.AllowedTools); got != 2 {
		t.Fatalf("len(AllowedTools) = %d want 2", got)
	}
	if got := len(cfg.DeniedTools); got != 1 {
		t.Fatalf("len(DeniedTools) = %d want 1", got)
	}
	if cfg.Passport != "./guardrails/passport.json" {
		t.Fatalf("Passport = %q", cfg.Passport)
	}
}

func TestLoadConfigFromEnvDefaults(t *testing.T) {
	for _, key := range []string{
		"DEER_FLOW_CONFIG_PATH",
		"DEERFLOW_GUARDRAILS_ENABLED",
		"DEERFLOW_GUARDRAILS_ALLOWED_TOOLS",
		"DEERFLOW_GUARDRAILS_DENIED_TOOLS",
		"DEERFLOW_GUARDRAILS_FAIL_CLOSED",
		"DEERFLOW_GUARDRAILS_PASSPORT",
	} {
		_ = os.Unsetenv(key)
	}

	cfg := LoadConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("Enabled = true want false")
	}
	if !cfg.FailClosed {
		t.Fatal("FailClosed = false want true")
	}
	if provider := cfg.BuildProvider(); provider != nil {
		t.Fatalf("BuildProvider() = %#v want nil", provider)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	data := `
guardrails:
  enabled: true
  fail_closed: false
  passport: ./policy/passport.json
  provider:
    use: deerflow.guardrails.builtin:AllowlistProvider
    config:
      allowed_tools:
        - web_search
        - read_file
      denied_tools:
        - bash
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(data)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)
	for _, key := range []string{
		"DEERFLOW_GUARDRAILS_ENABLED",
		"DEERFLOW_GUARDRAILS_ALLOWED_TOOLS",
		"DEERFLOW_GUARDRAILS_DENIED_TOOLS",
		"DEERFLOW_GUARDRAILS_FAIL_CLOSED",
		"DEERFLOW_GUARDRAILS_PASSPORT",
	} {
		t.Setenv(key, "")
	}

	cfg := LoadConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("Enabled = false want true")
	}
	if cfg.FailClosed {
		t.Fatal("FailClosed = true want false")
	}
	if cfg.Passport != "./policy/passport.json" {
		t.Fatalf("Passport = %q want ./policy/passport.json", cfg.Passport)
	}
	if got := len(cfg.AllowedTools); got != 2 {
		t.Fatalf("len(AllowedTools) = %d want 2", got)
	}
	if got := len(cfg.DeniedTools); got != 1 {
		t.Fatalf("len(DeniedTools) = %d want 1", got)
	}
}

func TestLoadConfigFromFileSupportsModernEnvName(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	data := `
guardrails:
  enabled: true
  provider:
    use: deerflow.guardrails.builtin:AllowlistProvider
    config:
      denied_tools:
        - bash
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(data)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DEERFLOW_CONFIG_PATH", configPath)

	cfg := LoadConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("Enabled = false want true")
	}
	if got := len(cfg.DeniedTools); got != 1 || cfg.DeniedTools[0] != "bash" {
		t.Fatalf("DeniedTools = %#v want [bash]", cfg.DeniedTools)
	}
}

func TestLoadConfigFromEnvOverridesFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	data := `
guardrails:
  enabled: false
  fail_closed: true
  passport: ./policy/passport.json
  provider:
    use: deerflow.guardrails.builtin:AllowlistProvider
    config:
      denied_tools:
        - bash
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(data)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)
	t.Setenv("DEERFLOW_GUARDRAILS_ENABLED", "true")
	t.Setenv("DEERFLOW_GUARDRAILS_FAIL_CLOSED", "false")
	t.Setenv("DEERFLOW_GUARDRAILS_ALLOWED_TOOLS", "web_search")
	t.Setenv("DEERFLOW_GUARDRAILS_DENIED_TOOLS", "write_file")
	t.Setenv("DEERFLOW_GUARDRAILS_PASSPORT", "./override/passport.json")

	cfg := LoadConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("Enabled = false want true")
	}
	if cfg.FailClosed {
		t.Fatal("FailClosed = true want false")
	}
	if cfg.Passport != "./override/passport.json" {
		t.Fatalf("Passport = %q want ./override/passport.json", cfg.Passport)
	}
	if got := len(cfg.AllowedTools); got != 1 || cfg.AllowedTools[0] != "web_search" {
		t.Fatalf("AllowedTools = %#v want [web_search]", cfg.AllowedTools)
	}
	if got := len(cfg.DeniedTools); got != 1 || cfg.DeniedTools[0] != "write_file" {
		t.Fatalf("DeniedTools = %#v want [write_file]", cfg.DeniedTools)
	}
}

func TestBuildProviderFromConfigProviderArgs(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ProviderUse: "deerflow.guardrails.builtin:AllowlistProvider",
		ProviderArgs: map[string]any{
			"allowed_tools": []any{"web_search", "read_file"},
			"denied_tools":  []any{"bash"},
		},
	}

	provider := cfg.BuildProvider()
	if provider == nil {
		t.Fatal("BuildProvider() = nil want allowlist provider")
	}

	decision, err := provider.Evaluate(Request{ToolName: "bash"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Allow {
		t.Fatalf("decision.Allow = true want false")
	}
}

func TestBuildProviderRejectsUnsupportedProvider(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		ProviderUse: "example.com/custom:Provider",
	}

	provider := cfg.BuildProvider()
	if provider == nil {
		t.Fatal("BuildProvider() = nil want invalid provider")
	}

	_, err := provider.Evaluate(Request{ToolName: "bash"})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("Evaluate() error = %v want unsupported provider error", err)
	}
}
