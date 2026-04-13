package langgraphcmd

import (
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestConfigApplyYoloDefaults(t *testing.T) {
	cfg := Config{}
	cfg.ApplyYoloDefaults(true)
	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.Provider != "siliconflow" {
		t.Fatalf("Provider = %q", cfg.Provider)
	}
	if cfg.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("Model = %q", cfg.Model)
	}
}

func TestConfigStartupLines(t *testing.T) {
	cfg := DefaultConfig()
	lines := cfg.StartupLines(BuildInfo{Version: "v1", Commit: "abc", BuildTime: "now"}, false, "debug")
	if len(lines) == 0 {
		t.Fatal("StartupLines() returned no lines")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Starting deerflow-go server") {
		t.Fatalf("StartupLines() = %q", joined)
	}
	if !strings.Contains(joined, "Log Level: debug") {
		t.Fatalf("StartupLines() = %q", joined)
	}
}

func TestConfigReadyLinesForAllInOne(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Runtime.Role = harnessruntime.RuntimeNodeRoleAllInOne
	cfg.Runtime.Addr = "127.0.0.1:18081"
	joined := strings.Join(cfg.ReadyLines(), "\n")
	if !strings.Contains(joined, "Runtime worker: http://127.0.0.1:18081/dispatch") {
		t.Fatalf("ReadyLines() = %q", joined)
	}
}

func TestConfigReadyLinesForGateway(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Runtime.Role = harnessruntime.RuntimeNodeRoleGateway
	cfg.Runtime.Endpoint = "http://worker:8081/dispatch"
	joined := strings.Join(cfg.ReadyLines(), "\n")
	if !strings.Contains(joined, "Remote worker endpoint: http://worker:8081/dispatch") {
		t.Fatalf("ReadyLines() = %q", joined)
	}
}
