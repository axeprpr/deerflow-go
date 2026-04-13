package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestBuildDefaultRuntimeBootstrapBuildsDefaultRuntimePieces(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	bootstrap, err := BuildDefaultRuntimeBootstrap(config, nil, clarification.NewManager(4))
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeBootstrap() error = %v", err)
	}
	if bootstrap == nil || bootstrap.Node == nil {
		t.Fatalf("bootstrap = %#v", bootstrap)
	}
	if bootstrap.SandboxRuntime == nil {
		t.Fatal("SandboxRuntime = nil")
	}
	if bootstrap.ToolRuntime == nil || bootstrap.ToolRuntime.Registry() == nil {
		t.Fatalf("ToolRuntime = %#v", bootstrap.ToolRuntime)
	}
	if bootstrap.Node.ToolRuntime() != bootstrap.ToolRuntime {
		t.Fatalf("node tool runtime = %#v want %#v", bootstrap.Node.ToolRuntime(), bootstrap.ToolRuntime)
	}
	if bootstrap.Node.ToolRegistry() != bootstrap.ToolRuntime.Registry() {
		t.Fatalf("node tool registry = %#v want %#v", bootstrap.Node.ToolRegistry(), bootstrap.ToolRuntime.Registry())
	}
	if bootstrap.Node.Subagents() != bootstrap.ToolRuntime.Subagents() {
		t.Fatalf("node subagents = %#v want %#v", bootstrap.Node.Subagents(), bootstrap.ToolRuntime.Subagents())
	}
}

func TestBuildDefaultMemoryServiceBuildsMigratedRuntime(t *testing.T) {
	service, err := BuildDefaultMemoryService(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("BuildDefaultMemoryService() error = %v", err)
	}
	if service == nil || service.Runtime() == nil {
		t.Fatalf("service = %#v", service)
	}
	if !service.Enabled() {
		t.Fatal("memory service is not enabled")
	}
}

func TestBuildDefaultRuntimeBootstrapWithMemoryBuildsMemoryService(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	bootstrap, err := BuildDefaultRuntimeBootstrapWithMemory(context.Background(), config, t.TempDir(), nil, clarification.NewManager(4))
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeBootstrapWithMemory() error = %v", err)
	}
	if bootstrap == nil || bootstrap.Node == nil {
		t.Fatalf("bootstrap = %#v", bootstrap)
	}
	if bootstrap.MemoryService == nil || bootstrap.MemoryService.Runtime() == nil {
		t.Fatalf("MemoryService = %#v", bootstrap.MemoryService)
	}
	if bootstrap.MemoryErr != nil {
		t.Fatalf("MemoryErr = %v", bootstrap.MemoryErr)
	}
	if bootstrap.Node.MemoryService() != bootstrap.MemoryService {
		t.Fatalf("node memory service = %#v want %#v", bootstrap.Node.MemoryService(), bootstrap.MemoryService)
	}
	if bootstrap.Node.MemoryRuntime() != bootstrap.MemoryService.Runtime() {
		t.Fatalf("node memory runtime = %#v want %#v", bootstrap.Node.MemoryRuntime(), bootstrap.MemoryService.Runtime())
	}
}

func TestBuildDefaultHarnessRuntimeBindsRuntimeToNode(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	bootstrap, err := BuildDefaultRuntimeBootstrapWithMemory(context.Background(), config, t.TempDir(), nil, clarification.NewManager(4))
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeBootstrapWithMemory() error = %v", err)
	}
	runtime := BuildDefaultHarnessRuntime(bootstrap, nil, 100, func(memory *harness.MemoryRuntime, tools harness.ToolRuntime, sandbox harness.SandboxRuntime) harness.ProfileBuilder {
		return harness.NewStaticProfileBuilder(harness.RuntimeProfile{
			ToolRuntime:    tools,
			SandboxRuntime: sandbox,
		})
	})
	if runtime == nil {
		t.Fatal("runtime = nil")
	}
	if bootstrap.Node.RuntimeView() != runtime {
		t.Fatalf("node runtime = %#v want %#v", bootstrap.Node.RuntimeView(), runtime)
	}
}

func TestRuntimeBootstrapEnsureLauncherBindsDispatch(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	bootstrap, err := BuildDefaultRuntimeSystemWithMemory(context.Background(), config, t.TempDir(), nil, clarification.NewManager(4), 100, nil)
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeSystemWithMemory() error = %v", err)
	}
	launcher := bootstrap.EnsureLauncher(func() *harness.Runtime { return bootstrap.Runtime }, nil)
	if launcher == nil || launcher.Node() == nil {
		t.Fatalf("launcher = %#v", launcher)
	}
	if launcher.Node().RunDispatcher() == nil {
		t.Fatal("dispatcher = nil")
	}
}

func TestBuildDefaultRuntimeSystemWithMemoryBuildsRuntime(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	bootstrap, err := BuildDefaultRuntimeSystemWithMemory(context.Background(), config, t.TempDir(), nil, clarification.NewManager(4), 100, func(memory *harness.MemoryRuntime, tools harness.ToolRuntime, sandbox harness.SandboxRuntime) harness.ProfileBuilder {
		return harness.NewStaticProfileBuilder(harness.RuntimeProfile{
			ToolRuntime:    tools,
			SandboxRuntime: sandbox,
		})
	})
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeSystemWithMemory() error = %v", err)
	}
	if bootstrap == nil || bootstrap.Runtime == nil {
		t.Fatalf("bootstrap = %#v", bootstrap)
	}
	if bootstrap.Node.RuntimeView() != bootstrap.Runtime {
		t.Fatalf("node runtime = %#v want %#v", bootstrap.Node.RuntimeView(), bootstrap.Runtime)
	}
}

func TestRefreshHarnessRuntimePrefersCurrentOverrides(t *testing.T) {
	config := DefaultRuntimeNodeConfig("runtime-test", t.TempDir())
	bootstrap, err := BuildDefaultRuntimeBootstrapWithMemory(context.Background(), config, t.TempDir(), nil, clarification.NewManager(4))
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeBootstrapWithMemory() error = %v", err)
	}
	base := BuildDefaultHarnessRuntime(bootstrap, nil, 100, nil)
	if base == nil {
		t.Fatal("base runtime = nil")
	}
	overrideTools := harness.NewStaticToolRuntime(bootstrap.Node.ToolRegistry(), nil, nil)
	current := harness.NewRuntime(harness.RuntimeDeps{
		ToolRuntime:     overrideTools,
		DefaultMaxTurns: 100,
	}, bootstrap.Node.MemoryRuntime())
	refreshed := RefreshHarnessRuntime(bootstrap.Node, nil, 100, current, nil)
	if refreshed == nil {
		t.Fatal("refreshed runtime = nil")
	}
	if refreshed.ToolRuntime() != overrideTools {
		t.Fatalf("refreshed tool runtime = %#v want %#v", refreshed.ToolRuntime(), overrideTools)
	}
	if bootstrap.Node.RuntimeView() != refreshed {
		t.Fatalf("node runtime = %#v want %#v", bootstrap.Node.RuntimeView(), refreshed)
	}
}
