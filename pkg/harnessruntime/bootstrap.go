package harnessruntime

import (
	"context"
	"path/filepath"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

type RuntimeBootstrap struct {
	Node           *RuntimeNode
	SandboxRuntime harness.SandboxRuntime
	ToolRuntime    harness.ToolRuntime
}

func BuildDefaultRuntimeBootstrap(config RuntimeNodeConfig, provider llm.LLMProvider, clarify *clarification.Manager) (*RuntimeBootstrap, error) {
	node, err := config.BuildRuntimeNode(DispatchRuntimeConfig{})
	if err != nil {
		return nil, err
	}
	sandboxRuntime := node.ConfiguredSandboxRuntime()
	return &RuntimeBootstrap{
		Node:           node,
		SandboxRuntime: sandboxRuntime,
		ToolRuntime:    NewDefaultToolRuntime(provider, clarify, sandboxRuntime),
	}, nil
}

func BuildDefaultMemoryService(ctx context.Context, dataRoot string) (*MemoryService, error) {
	store, err := pkgmemory.NewFileStore(filepath.Join(dataRoot, "memory"))
	if err != nil {
		return nil, err
	}
	if err := store.AutoMigrate(ctx); err != nil {
		return nil, err
	}
	return NewMemoryService(store, nil), nil
}
