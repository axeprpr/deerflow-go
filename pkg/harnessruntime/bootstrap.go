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

type RuntimeProfileBuilderFactory func(*harness.MemoryRuntime, harness.ToolRuntime, harness.SandboxRuntime) harness.ProfileBuilder

func BuildDefaultRuntimeBootstrap(config RuntimeNodeConfig, provider llm.LLMProvider, clarify *clarification.Manager) (*RuntimeBootstrap, error) {
	node, err := config.BuildRuntimeNode(DispatchRuntimeConfig{})
	if err != nil {
		return nil, err
	}
	sandboxRuntime := node.ConfiguredSandboxRuntime()
	toolRuntime := NewDefaultToolRuntime(provider, clarify, sandboxRuntime)
	node.BindToolRuntime(toolRuntime)
	return &RuntimeBootstrap{
		Node:           node,
		SandboxRuntime: sandboxRuntime,
		ToolRuntime:    toolRuntime,
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

func BuildDefaultHarnessRuntime(bootstrap *RuntimeBootstrap, provider llm.LLMProvider, maxTurns int, profileBuilder RuntimeProfileBuilderFactory) *harness.Runtime {
	if bootstrap == nil {
		return nil
	}
	node := bootstrap.Node
	var memoryRuntime *harness.MemoryRuntime
	if bootstrap.MemoryService != nil {
		memoryRuntime = bootstrap.MemoryService.Runtime()
	}
	toolRuntime := bootstrap.ToolRuntime
	sandboxRuntime := bootstrap.SandboxRuntime
	if node != nil {
		if runtime := node.MemoryRuntime(); runtime != nil {
			memoryRuntime = runtime
		}
		if runtime := node.ToolRuntime(); runtime != nil {
			toolRuntime = runtime
		}
		if runtime := node.ConfiguredSandboxRuntime(); runtime != nil {
			sandboxRuntime = runtime
		}
	}
	var sandboxProvider harness.SandboxProvider
	if sandboxRuntime != nil {
		sandboxProvider = sandboxRuntime.Provider()
	}
	var options []harness.RuntimeOption
	if profileBuilder != nil {
		options = append(options, harness.WithProfileBuilder(profileBuilder(memoryRuntime, toolRuntime, sandboxRuntime)))
	}
	runtime := harness.NewRuntime(harness.RuntimeDeps{
		LLMProvider:     provider,
		Tools:           toolRuntime.Registry(),
		ToolRuntime:     toolRuntime,
		DefaultMaxTurns: maxTurns,
		ProfileResolver: NewModeProfileResolver(),
		SandboxRuntime:  sandboxRuntime,
		SandboxProvider: sandboxProvider,
	}, memoryRuntime, options...)
	if node != nil {
		node.BindRuntime(runtime)
	}
	return runtime
}
