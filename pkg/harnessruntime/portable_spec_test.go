package harnessruntime

import (
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestPortableAgentSpecRoundTripPreservesPortableFields(t *testing.T) {
	temp := 0.3
	maxTokens := 1024
	failClosed := true
	spec := harness.AgentSpec{
		ExecutionMode:          "background",
		AgentType:              agent.AgentTypeGeneral,
		MaxTurns:               9,
		MaxConcurrentSubagents: 2,
		Model:                  "model-1",
		ReasoningEffort:        "medium",
		SystemPrompt:           "system",
		Temperature:            &temp,
		MaxTokens:              &maxTokens,
		RequestTimeout:         2 * time.Minute,
		GuardrailFailClosed:    &failClosed,
		GuardrailPassport:      "passport-1",
	}

	port := PortableAgentSpecFromAgentSpec(spec)
	got := port.AgentSpec()

	if got.ExecutionMode != "background" || got.Model != "model-1" || got.SystemPrompt != "system" {
		t.Fatalf("AgentSpec() = %#v", got)
	}
	if got.MaxTurns != 9 || got.MaxConcurrentSubagents != 2 {
		t.Fatalf("AgentSpec() limits = %#v", got)
	}
	if got.RequestTimeout != 2*time.Minute {
		t.Fatalf("AgentSpec() timeout = %s", got.RequestTimeout)
	}
	if got.Temperature == nil || *got.Temperature != temp {
		t.Fatalf("AgentSpec() temperature = %#v", got.Temperature)
	}
	if got.MaxTokens == nil || *got.MaxTokens != maxTokens {
		t.Fatalf("AgentSpec() maxTokens = %#v", got.MaxTokens)
	}
	if got.GuardrailPassport != "passport-1" {
		t.Fatalf("AgentSpec() passport = %q", got.GuardrailPassport)
	}
}
