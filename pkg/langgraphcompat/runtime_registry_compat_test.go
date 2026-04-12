package langgraphcompat

import (
	"context"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
)

func newRuntimeToolRegistry(t *testing.T) *tools.Registry {
	t.Helper()

	registry := tools.NewRegistry()
	manager := clarification.NewManager(8)
	for _, tool := range builtin.FileTools() {
		mustRegisterTool(t, registry, tool)
	}
	mustRegisterTool(t, registry, builtin.BashTool())
	mustRegisterTool(t, registry, clarification.AskClarificationTool(manager))
	mustRegisterTool(t, registry, tools.TaskTool(agent.NewSubagentPool(noopLLMProvider{}, registry, nil, nil, 2, 2*time.Minute)))
	return registry
}

func mustRegisterTool(t *testing.T, registry *tools.Registry, tool models.Tool) {
	t.Helper()
	if err := registry.Register(tool); err != nil {
		t.Fatalf("register tool %q: %v", tool.Name, err)
	}
}

type noopLLMProvider struct{}

func (noopLLMProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (noopLLMProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}
