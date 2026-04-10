package llm

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveEinoRequestTimeoutDefaultsToTenMinutes(t *testing.T) {
	t.Setenv("DEERFLOW_LLM_REQUEST_TIMEOUT", "")
	t.Setenv("DEERFLOW_CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("DEER_FLOW_CONFIG_PATH", "")

	if got := resolveEinoRequestTimeout(); got != 10*time.Minute {
		t.Fatalf("timeout=%v want=%v", got, 10*time.Minute)
	}
}

func TestResolveEinoRequestTimeoutUsesConfigRequestTimeout(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	config := []byte("models:\n  - name: qwen3.5-27b\n    request_timeout: 600\n  - name: other\n    timeout: 120\n")
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	t.Setenv("DEERFLOW_CONFIG_PATH", configPath)
	t.Setenv("DEER_FLOW_CONFIG_PATH", "")
	t.Setenv("DEERFLOW_LLM_REQUEST_TIMEOUT", "")

	if got := resolveEinoRequestTimeout(); got != 10*time.Minute {
		t.Fatalf("timeout=%v want=%v", got, 10*time.Minute)
	}
}

func TestResolveEinoRequestTimeoutEnvOverrideWins(t *testing.T) {
	t.Setenv("DEERFLOW_LLM_REQUEST_TIMEOUT", "42")
	t.Setenv("DEERFLOW_CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("DEER_FLOW_CONFIG_PATH", "")

	if got := resolveEinoRequestTimeout(); got != 42*time.Second {
		t.Fatalf("timeout=%v want=%v", got, 42*time.Second)
	}
}
