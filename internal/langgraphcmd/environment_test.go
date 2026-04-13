package langgraphcmd

import "testing"

func TestConfigApplyYoloEnvironment(t *testing.T) {
	cfg := Config{}
	cfg.ApplyYoloEnvironment(true)
	if got := getenv("DEERFLOW_YOLO"); got != "1" {
		t.Fatalf("DEERFLOW_YOLO = %q", got)
	}
	if got := getenv("ADDR"); got != ":8080" {
		t.Fatalf("ADDR = %q", got)
	}
	if cfg.Provider != "siliconflow" || cfg.Model != "qwen/Qwen3.5-9B" || cfg.Addr != ":8080" {
		t.Fatalf("cfg after yolo = %#v", cfg)
	}
}
