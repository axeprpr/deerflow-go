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
	MemoryService  *MemoryService
	MemoryErr      error
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

func BuildDefaultRuntimeBootstrapWithMemory(ctx context.Context, config RuntimeNodeConfig, dataRoot string, provider llm.LLMProvider, clarify *clarification.Manager) (*RuntimeBootstrap, error) {
	bootstrap, err := BuildDefaultRuntimeBootstrap(config, provider, clarify)
	if err != nil {
		return nil, err
	}
	memoryService, err := BuildDefaultMemoryService(ctx, dataRoot)
	if err == nil {
		bootstrap.MemoryService = memoryService
		bootstrap.Node.BindMemoryService(memoryService)
	} else {
		bootstrap.MemoryErr = err
	}
	return bootstrap, nil
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
