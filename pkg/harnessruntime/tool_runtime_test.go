package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/llm"
)

func TestNewDefaultToolRuntimeBuildsExpectedRegistry(t *testing.T) {
	runtime := NewDefaultToolRuntime(noopLLMProvider{}, clarification.NewManager(4), nil)
	if runtime == nil {
		t.Fatal("runtime=nil")
	}
	if runtime.Registry() == nil {
		t.Fatal("registry=nil")
	}
	if runtime.Subagents() == nil {
		t.Fatal("subagents=nil")
	}

	got := make([]string, 0, len(runtime.Registry().List()))
	for _, tool := range runtime.Registry().List() {
		got = append(got, tool.Name)
	}

	want := []string{"ls", "read_file", "glob", "grep", "write_file", "str_replace", "web_search", "web_fetch", "image_search", "bash", "ask_clarification", "task"}
	if len(got) != len(want) {
		t.Fatalf("tools=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tools=%v want=%v", got, want)
		}
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
