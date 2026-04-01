package langgraphcompat

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/subagent"
)

func TestLoadSubagentsAppConfigDefaults(t *testing.T) {
	t.Setenv("DEER_FLOW_CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yaml"))

	cfg := loadSubagentsAppConfig()
	if got := cfg.timeoutFor(subagent.SubagentGeneralPurpose); got != defaultGatewaySubagentTimeout {
		t.Fatalf("general timeout=%s want=%s", got, defaultGatewaySubagentTimeout)
	}
	if got := cfg.timeoutFor(subagent.SubagentBash); got != defaultGatewaySubagentTimeout {
		t.Fatalf("bash timeout=%s want=%s", got, defaultGatewaySubagentTimeout)
	}
}

func TestLoadSubagentsAppConfigFromConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
subagents:
  timeout_seconds: 600
  agents:
    bash:
      timeout_seconds: 45
    general-purpose:
      timeout_seconds: 90
    unknown:
      timeout_seconds: 30
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

	cfg := loadSubagentsAppConfig()
	if got := cfg.timeoutFor(subagent.SubagentGeneralPurpose); got != 90*time.Second {
		t.Fatalf("general timeout=%s want=%s", got, 90*time.Second)
	}
	if got := cfg.timeoutFor(subagent.SubagentBash); got != 45*time.Second {
		t.Fatalf("bash timeout=%s want=%s", got, 45*time.Second)
	}
}

func TestLoadSubagentsAppConfigFallsBackToGlobalTimeout(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
subagents:
  timeout_seconds: 480
  agents:
    bash:
      timeout_seconds: 0
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEER_FLOW_CONFIG_PATH", configPath)

	cfg := loadSubagentsAppConfig()
	if got := cfg.timeoutFor(subagent.SubagentGeneralPurpose); got != 480*time.Second {
		t.Fatalf("general timeout=%s want=%s", got, 480*time.Second)
	}
	if got := cfg.timeoutFor(subagent.SubagentBash); got != 480*time.Second {
		t.Fatalf("bash timeout=%s want=%s", got, 480*time.Second)
	}
}
