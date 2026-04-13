package langgraphcmd

import (
	"strings"
	"testing"
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
