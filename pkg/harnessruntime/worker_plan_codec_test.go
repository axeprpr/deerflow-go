package harnessruntime

import (
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func TestWorkerPlanCodecRoundTripPortablePlan(t *testing.T) {
	temp := 0.1
	maxTokens := 2048
	registry := tools.NewPresentFileRegistry()
	plan := NewWorkerExecutionPlan(RunPlan{
		ThreadID:        "thread-1",
		AssistantID:     "lead_agent",
		RunID:           "run-1",
		SubmittedAt:     time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC),
		Attempt:         2,
		ResumeFromEvent: 7,
		ResumeReason:    "resume",
		Model:           "model-1",
		AgentName:       "lead_agent",
		Spec: harness.AgentSpec{
			ExecutionMode: "background",
			AgentType:     agent.AgentTypeGeneral,
			SystemPrompt:  "system",
			PinnedFacts: map[string]string{
				"tender_id": "TB-2026-041",
			},
			Temperature:  &temp,
			MaxTokens:    &maxTokens,
			PresentFiles: registry,
		},
		Features:         harness.FeatureSet{Sandbox: true, Memory: true},
		ExistingMessages: []models.Message{{Role: models.RoleHuman, Content: "old"}},
		Messages:         []models.Message{{Role: models.RoleHuman, Content: "new"}},
	})

	codec := WorkerPlanCodec{}
	data, err := codec.Encode(plan)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if strings.Contains(string(data), "PresentFiles") || strings.Contains(string(data), "DeferredTools") || strings.Contains(string(data), "GuardrailProvider") {
		t.Fatalf("encoded plan leaked runtime-only fields: %s", string(data))
	}

	decoded, err := codec.Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.ThreadID != "thread-1" || decoded.RunID != "run-1" || decoded.ResumeFromEvent != 7 {
		t.Fatalf("decoded plan = %#v", decoded)
	}
	if !decoded.Features.Sandbox || !decoded.Features.Memory {
		t.Fatalf("decoded features = %#v", decoded.Features)
	}
	if decoded.Spec.ExecutionMode != "background" || decoded.Spec.SystemPrompt != "system" {
		t.Fatalf("decoded spec = %#v", decoded.Spec)
	}
	if decoded.Spec.PinnedFacts["tender_id"] != "TB-2026-041" {
		t.Fatalf("decoded pinned facts = %#v", decoded.Spec.PinnedFacts)
	}
	if len(decoded.Messages) != 1 || decoded.Messages[0].Content != "new" {
		t.Fatalf("decoded messages = %#v", decoded.Messages)
	}
}
