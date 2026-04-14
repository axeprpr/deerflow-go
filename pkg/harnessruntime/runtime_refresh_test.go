package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/llm"
)

type taggedLLMProvider struct {
	tag string
}

func (p *taggedLLMProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Model: p.tag}, nil
}

func (p *taggedLLMProvider) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func TestRuntimeNodeRefreshRuntimeViewReusesWhenInputsUnchanged(t *testing.T) {
	node := &RuntimeNode{}
	provider := &taggedLLMProvider{tag: "same"}

	first := node.RefreshRuntimeView(provider, 8, nil, nil)
	if first == nil {
		t.Fatal("first runtime = nil")
	}
	second := node.RefreshRuntimeView(provider, 8, first, nil)
	if second != first {
		t.Fatalf("expected runtime reuse, first=%p second=%p", first, second)
	}
	if node.RuntimeView() != first {
		t.Fatalf("node runtime view = %p, want %p", node.RuntimeView(), first)
	}
}

func TestRuntimeNodeRefreshRuntimeViewRebuildsWhenInputsChange(t *testing.T) {
	node := &RuntimeNode{}
	providerA := &taggedLLMProvider{tag: "a"}
	providerB := &taggedLLMProvider{tag: "b"}

	first := node.RefreshRuntimeView(providerA, 8, nil, nil)
	second := node.RefreshRuntimeView(providerB, 8, first, nil)
	if second == first {
		t.Fatalf("provider change should rebuild runtime, first=%p second=%p", first, second)
	}

	third := node.RefreshRuntimeView(providerB, 9, second, nil)
	if third == second {
		t.Fatalf("maxTurns change should rebuild runtime, second=%p third=%p", second, third)
	}

	profileA := RuntimeProfileBuilderFactory(func(*harness.MemoryRuntime, harness.ToolRuntime, harness.SandboxRuntime) harness.ProfileBuilder {
		return nil
	})
	profileB := RuntimeProfileBuilderFactory(func(*harness.MemoryRuntime, harness.ToolRuntime, harness.SandboxRuntime) harness.ProfileBuilder {
		return nil
	})
	fourth := node.RefreshRuntimeView(providerB, 9, third, profileA)
	fifth := node.RefreshRuntimeView(providerB, 9, fourth, profileB)
	if fifth == fourth {
		t.Fatalf("profile builder change should rebuild runtime, fourth=%p fifth=%p", fourth, fifth)
	}
}
