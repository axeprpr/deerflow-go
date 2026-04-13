package main

import "testing"

func TestDefaultConfigUsesEnvironmentDefaults(t *testing.T) {
	t.Setenv("DEERFLOW_AUTH_TOKEN", "token")
	t.Setenv("ADDR", ":9090")
	t.Setenv("DATABASE_URL", "postgres://db")
	t.Setenv("DEFAULT_LLM_PROVIDER", "openai")
	t.Setenv("DEFAULT_LLM_MODEL", "gpt-4.1-mini")

	cfg := defaultConfig()
	if cfg.AuthToken != "token" {
		t.Fatalf("AuthToken = %q", cfg.AuthToken)
	}
	if cfg.Addr != ":9090" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.DatabaseURL != "postgres://db" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("Provider = %q", cfg.Provider)
	}
	if cfg.Model != "gpt-4.1-mini" {
		t.Fatalf("Model = %q", cfg.Model)
	}
}

func TestDefaultConfigFallsBackToBuiltins(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Provider != "siliconflow" {
		t.Fatalf("Provider = %q, want %q", cfg.Provider, "siliconflow")
	}
	if cfg.Model != "qwen/Qwen3.5-9B" {
		t.Fatalf("Model = %q, want %q", cfg.Model, "qwen/Qwen3.5-9B")
	}
}
