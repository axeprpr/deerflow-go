package langgraphcompat

import (
	"testing"
	"time"
)

func TestResolveRunConfigUsesGatewayProviderModelForAlias(t *testing.T) {
	s := &Server{
		defaultModel: "qwen3.5-27b",
		models: map[string]gatewayModel{
			"qwen3.5-27b": {
				ID:                      "qwen3.5-27b",
				Name:                    "qwen3.5-27b",
				Model:                   "Qwen/Qwen3.5-27B",
				DisplayName:             "Qwen 3.5 27B",
				SupportsThinking:        false,
				SupportsReasoningEffort: false,
			},
		},
	}

	cfg, err := s.resolveRunConfig(runConfig{
		ModelName:       "qwen3.5-27b",
		ReasoningEffort: "minimal",
	}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.Model != "Qwen/Qwen3.5-27B" {
		t.Fatalf("model=%q want=%q", cfg.Model, "Qwen/Qwen3.5-27B")
	}
	if cfg.ReasoningEffort != "" {
		t.Fatalf("reasoning_effort=%q want empty for unsupported model", cfg.ReasoningEffort)
	}
}

func TestResolveRunConfigResolvesDefaultModelAliasToProviderModel(t *testing.T) {
	s := &Server{
		defaultModel: "qwen3.5-27b",
		models: map[string]gatewayModel{
			"qwen3.5-27b": {
				ID:                      "qwen3.5-27b",
				Name:                    "qwen3.5-27b",
				Model:                   "Qwen/Qwen3.5-27B",
				DisplayName:             "Qwen 3.5 27B",
				SupportsThinking:        false,
				SupportsReasoningEffort: false,
			},
		},
	}

	cfg, err := s.resolveRunConfig(runConfig{}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.Model != "Qwen/Qwen3.5-27B" {
		t.Fatalf("model=%q want=%q", cfg.Model, "Qwen/Qwen3.5-27B")
	}
}

func TestResolveRunConfigKeepsReasoningEffortForSupportedGatewayModel(t *testing.T) {
	s := &Server{
		defaultModel: "gpt-5",
		models: map[string]gatewayModel{
			"gpt-5": {
				ID:                      "gpt-5",
				Name:                    "gpt-5",
				Model:                   "openai/gpt-5",
				DisplayName:             "GPT-5",
				SupportsThinking:        true,
				SupportsReasoningEffort: true,
			},
		},
	}

	cfg, err := s.resolveRunConfig(runConfig{
		ModelName:       "gpt-5",
		ReasoningEffort: "high",
	}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.Model != "openai/gpt-5" {
		t.Fatalf("model=%q want=%q", cfg.Model, "openai/gpt-5")
	}
	if cfg.ReasoningEffort != "high" {
		t.Fatalf("reasoning_effort=%q want=%q", cfg.ReasoningEffort, "high")
	}
}

func TestResolveRunConfigUsesGatewayModelRequestTimeout(t *testing.T) {
	s := &Server{
		defaultModel: "qwen3.5-27b",
		models: map[string]gatewayModel{
			"qwen3.5-27b": {
				ID:                    "qwen3.5-27b",
				Name:                  "qwen3.5-27b",
				Model:                 "Qwen/Qwen3.5-27B",
				DisplayName:           "Qwen 3.5 27B",
				RequestTimeoutSeconds: 600,
			},
		},
	}

	cfg, err := s.resolveRunConfig(runConfig{ModelName: "qwen3.5-27b"}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.RequestTimeout != 10*time.Minute {
		t.Fatalf("request_timeout=%v want=%v", cfg.RequestTimeout, 10*time.Minute)
	}
}

func TestResolveRunConfigUsesRecursionLimitAsMaxTurns(t *testing.T) {
	s := &Server{
		defaultModel: "qwen3.5-27b",
		maxTurns:     100,
		models: map[string]gatewayModel{
			"qwen3.5-27b": {
				ID:    "qwen3.5-27b",
				Name:  "qwen3.5-27b",
				Model: "Qwen/Qwen3.5-27B",
			},
		},
	}
	maxTurns := 250

	cfg, err := s.resolveRunConfig(runConfig{
		ModelName: "qwen3.5-27b",
		MaxTurns:  &maxTurns,
	}, nil)
	if err != nil {
		t.Fatalf("resolveRunConfig error: %v", err)
	}
	if cfg.MaxTurns != 250 {
		t.Fatalf("max_turns=%d want=%d", cfg.MaxTurns, 250)
	}
}
