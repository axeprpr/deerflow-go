package harnessruntime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type RuntimeBootstrap struct {
	Node           *RuntimeNode
	SandboxRuntime harness.SandboxRuntime
	ToolRuntime    harness.ToolRuntime
	MemoryService  *MemoryService
	MemoryErr      error
	Runtime        *harness.Runtime
}

func (b *RuntimeBootstrap) Launcher() *RuntimeNodeLauncher {
	if b == nil {
		return nil
	}
	return NewRuntimeNodeLauncher(b.Node)
}

func (b *RuntimeBootstrap) EnsureLauncher(source func() *harness.Runtime, specs WorkerSpecRuntime) *RuntimeNodeLauncher {
	if b == nil || b.Node == nil {
		return NewRuntimeNodeLauncher(nil)
	}
	if source == nil && b.Runtime != nil {
		source = func() *harness.Runtime { return b.Runtime }
	}
	b.Node.EnsureDispatchSource(source, specs)
	return b.Launcher()
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
	memoryService, err := BuildDefaultMemoryService(ctx, dataRoot, config.Memory)
	if err == nil {
		bootstrap.MemoryService = memoryService
		bootstrap.Node.BindMemoryService(memoryService)
	} else {
		bootstrap.MemoryErr = err
	}
	return bootstrap, nil
}

func BuildDefaultMemoryService(ctx context.Context, dataRoot string, config RuntimeMemoryConfig) (*MemoryService, error) {
	storeURL := strings.TrimSpace(config.StoreURL)
	if storeURL != "" {
		store, err := pkgmemory.OpenStore(ctx, storeURL)
		if err != nil {
			return nil, err
		}
		return NewMemoryService(store, nil), nil
	}
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
	runtime := RefreshHarnessRuntime(bootstrap.Node, provider, maxTurns, bootstrap.Runtime, profileBuilder)
	bootstrap.Runtime = runtime
	return runtime
}

func BuildDefaultRuntimeSystemWithMemory(ctx context.Context, config RuntimeNodeConfig, dataRoot string, provider llm.LLMProvider, clarify *clarification.Manager, maxTurns int, profileBuilder RuntimeProfileBuilderFactory) (*RuntimeBootstrap, error) {
	bootstrap, err := BuildDefaultRuntimeBootstrapWithMemory(ctx, config, dataRoot, provider, clarify)
	if err != nil {
		return nil, err
	}
	BuildDefaultHarnessRuntime(bootstrap, provider, maxTurns, profileBuilder)
	return bootstrap, nil
}

func BuildDefaultRuntimeSystemLauncherWithMemory(ctx context.Context, config RuntimeNodeConfig, dataRoot string, provider llm.LLMProvider, clarify *clarification.Manager, maxTurns int, profileBuilder RuntimeProfileBuilderFactory, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeBootstrap, *RuntimeNodeLauncher, error) {
	bootstrap, err := BuildDefaultRuntimeSystemWithMemory(ctx, config, dataRoot, provider, clarify, maxTurns, profileBuilder)
	if err != nil {
		return nil, nil, err
	}
	return bootstrap, bootstrap.EnsureLauncher(source, specs), nil
}

func BuildDefaultRuntimeSystemLauncherForRoleWithMemory(ctx context.Context, role RuntimeNodeRole, name, root, endpoint, dataRoot string, provider llm.LLMProvider, clarify *clarification.Manager, maxTurns int, profileBuilder RuntimeProfileBuilderFactory, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeBootstrap, *RuntimeNodeLauncher, error) {
	switch role {
	case RuntimeNodeRoleAllInOne:
		return BuildDefaultAllInOneRuntimeSystemLauncherWithMemory(ctx, name, root, dataRoot, provider, clarify, maxTurns, profileBuilder, source, specs)
	case RuntimeNodeRoleGateway:
		return BuildDefaultGatewayRuntimeSystemLauncherWithMemory(ctx, name, root, endpoint, dataRoot, provider, clarify, maxTurns, profileBuilder, source, specs)
	case RuntimeNodeRoleWorker:
		return BuildDefaultWorkerRuntimeSystemLauncherWithMemory(ctx, name, root, dataRoot, provider, clarify, maxTurns, profileBuilder, source, specs)
	default:
		return nil, nil, fmt.Errorf("unsupported runtime node role %q", role)
	}
}

func BuildDefaultAllInOneRuntimeSystemLauncherWithMemory(ctx context.Context, name, root, dataRoot string, provider llm.LLMProvider, clarify *clarification.Manager, maxTurns int, profileBuilder RuntimeProfileBuilderFactory, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeBootstrap, *RuntimeNodeLauncher, error) {
	return BuildDefaultRuntimeSystemLauncherWithMemory(ctx, DefaultRuntimeNodeConfig(name, root), dataRoot, provider, clarify, maxTurns, profileBuilder, source, specs)
}

func BuildDefaultGatewayRuntimeSystemLauncherWithMemory(ctx context.Context, name, root, endpoint, dataRoot string, provider llm.LLMProvider, clarify *clarification.Manager, maxTurns int, profileBuilder RuntimeProfileBuilderFactory, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeBootstrap, *RuntimeNodeLauncher, error) {
	return BuildDefaultRuntimeSystemLauncherWithMemory(ctx, DefaultGatewayRuntimeNodeConfig(name, root, endpoint), dataRoot, provider, clarify, maxTurns, profileBuilder, source, specs)
}

func BuildDefaultWorkerRuntimeSystemLauncherWithMemory(ctx context.Context, name, root, dataRoot string, provider llm.LLMProvider, clarify *clarification.Manager, maxTurns int, profileBuilder RuntimeProfileBuilderFactory, source func() *harness.Runtime, specs WorkerSpecRuntime) (*RuntimeBootstrap, *RuntimeNodeLauncher, error) {
	return BuildDefaultRuntimeSystemLauncherWithMemory(ctx, DefaultWorkerRuntimeNodeConfig(name, root), dataRoot, provider, clarify, maxTurns, profileBuilder, source, specs)
}

func buildHarnessRuntime(node *RuntimeNode, provider llm.LLMProvider, maxTurns int, current *harness.Runtime, profileBuilder RuntimeProfileBuilderFactory) *harness.Runtime {
	var (
		memoryRuntime  *harness.MemoryRuntime
		sandboxRuntime harness.SandboxRuntime
		toolRuntime    harness.ToolRuntime
	)
	if node != nil {
		memoryRuntime = node.MemoryRuntime()
		sandboxRuntime = node.ConfiguredSandboxRuntime()
		toolRuntime = node.ToolRuntime()
	}
	if current != nil {
		if runtime := current.Memory(); runtime != nil {
			memoryRuntime = runtime
		}
		if runtime := current.SandboxRuntime(); runtime != nil {
			sandboxRuntime = runtime
		}
		if runtime := current.ToolRuntime(); runtime != nil {
			toolRuntime = runtime
		}
	}
	var sandboxProvider harness.SandboxProvider
	if sandboxRuntime != nil {
		sandboxProvider = sandboxRuntime.Provider()
	}
	var toolRegistry *tools.Registry
	if toolRuntime != nil {
		toolRegistry = toolRuntime.Registry()
	}
	var options []harness.RuntimeOption
	if profileBuilder != nil {
		options = append(options, harness.WithProfileBuilder(profileBuilder(memoryRuntime, toolRuntime, sandboxRuntime)))
	}
	return harness.NewRuntime(harness.RuntimeDeps{
		LLMProvider:     provider,
		Tools:           toolRegistry,
		ToolRuntime:     toolRuntime,
		DefaultMaxTurns: maxTurns,
		ProfileResolver: NewModeProfileResolver(),
		SandboxRuntime:  sandboxRuntime,
		SandboxProvider: sandboxProvider,
	}, memoryRuntime, options...)
}

func RefreshHarnessRuntime(node *RuntimeNode, provider llm.LLMProvider, maxTurns int, current *harness.Runtime, profileBuilder RuntimeProfileBuilderFactory) *harness.Runtime {
	runtime := buildHarnessRuntime(node, provider, maxTurns, current, profileBuilder)
	if node != nil {
		node.BindRuntime(runtime)
	}
	return runtime
}
