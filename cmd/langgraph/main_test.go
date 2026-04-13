package main

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDefaultConfigUsesEnvironmentDefaults(t *testing.T) {
	t.Setenv("DEERFLOW_AUTH_TOKEN", "token")
	t.Setenv("ADDR", ":9090")
	t.Setenv("DATABASE_URL", "postgres://db")
	t.Setenv("DEFAULT_LLM_PROVIDER", "openai")
	t.Setenv("DEFAULT_LLM_MODEL", "gpt-4.1-mini")
	t.Setenv("RUNTIME_NODE_ROLE", "gateway")
	t.Setenv("RUNTIME_NODE_ENDPOINT", "http://worker:8081/dispatch")
	t.Setenv("RUNTIME_NODE_MAX_TURNS", "77")

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
	if cfg.Runtime.Role != harnessruntime.RuntimeNodeRoleGateway {
		t.Fatalf("Runtime.Role = %q", cfg.Runtime.Role)
	}
	if cfg.Runtime.Endpoint != "http://worker:8081/dispatch" {
		t.Fatalf("Runtime.Endpoint = %q", cfg.Runtime.Endpoint)
	}
	if cfg.Runtime.MaxTurns != 77 {
		t.Fatalf("Runtime.MaxTurns = %d", cfg.Runtime.MaxTurns)
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
	if cfg.Runtime.Role != harnessruntime.RuntimeNodeRoleAllInOne {
		t.Fatalf("Runtime.Role = %q", cfg.Runtime.Role)
	}
	if cfg.Runtime.Addr != ":8081" {
		t.Fatalf("Runtime.Addr = %q", cfg.Runtime.Addr)
	}
}
