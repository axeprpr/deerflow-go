package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
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
	service, err := BuildDefaultMemoryService(context.Background(), t.TempDir(), RuntimeMemoryConfig{})
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

func TestBuildDefaultMemoryServiceUsesConfiguredStoreURL(t *testing.T) {
	root := t.TempDir()
	service, err := BuildDefaultMemoryService(context.Background(), root, RuntimeMemoryConfig{
		StoreURL: "sqlite://" + root + "/memory.sqlite3",
	})
	if err != nil {
		t.Fatalf("BuildDefaultMemoryService() error = %v", err)
	}
	if service == nil || service.Runtime() == nil {
		t.Fatalf("service = %#v", service)
	}
	if _, ok := service.Store().(*pkgmemory.SQLiteStore); !ok {
		t.Fatalf("store = %T, want *memory.SQLiteStore", service.Store())
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

func TestBuildDefaultRuntimeSystemLauncherWithMemoryBuildsLauncher(t *testing.T) {
	bootstrap, launcher, err := BuildDefaultRuntimeSystemLauncherWithMemory(context.Background(), DefaultRuntimeNodeConfig("runtime-test", t.TempDir()), t.TempDir(), nil, clarification.NewManager(4), 100, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeSystemLauncherWithMemory() error = %v", err)
	}
	if bootstrap == nil || bootstrap.Runtime == nil {
		t.Fatalf("bootstrap = %#v", bootstrap)
	}
	if launcher == nil || launcher.Node() == nil {
		t.Fatalf("launcher = %#v", launcher)
	}
	if launcher.Node().RunDispatcher() == nil {
		t.Fatal("dispatcher = nil")
	}
}

func TestRuntimeBootstrapEnsureLauncherFallsBackToBootstrapRuntime(t *testing.T) {
	bootstrap, err := BuildDefaultRuntimeSystemWithMemory(context.Background(), DefaultRuntimeNodeConfig("runtime-test", t.TempDir()), t.TempDir(), nil, clarification.NewManager(4), 100, nil)
	if err != nil {
		t.Fatalf("BuildDefaultRuntimeSystemWithMemory() error = %v", err)
	}
	launcher := bootstrap.EnsureLauncher(nil, nil)
	if launcher == nil || launcher.Node() == nil {
		t.Fatalf("launcher = %#v", launcher)
	}
	result, err := launcher.Node().RunDispatcher().Dispatch(context.Background(), DispatchRequest{
		Plan: WorkerExecutionPlan{RunID: "run-bootstrap-runtime", ThreadID: "thread-bootstrap-runtime"},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result == nil || result.Lifecycle == nil || result.Lifecycle.ThreadID != "thread-bootstrap-runtime" {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildDefaultGatewayRuntimeSystemLauncherWithMemoryUsesGatewayRole(t *testing.T) {
	bootstrap, launcher, err := BuildDefaultGatewayRuntimeSystemLauncherWithMemory(context.Background(), "runtime-test", t.TempDir(), "http://worker:8081/dispatch", t.TempDir(), nil, clarification.NewManager(4), 100, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildDefaultGatewayRuntimeSystemLauncherWithMemory() error = %v", err)
	}
	if bootstrap.Node.Config.Role != RuntimeNodeRoleGateway {
		t.Fatalf("role = %q, want %q", bootstrap.Node.Config.Role, RuntimeNodeRoleGateway)
	}
	if launcher.Spec().Role != RuntimeNodeRoleGateway {
		t.Fatalf("launcher role = %q, want %q", launcher.Spec().Role, RuntimeNodeRoleGateway)
	}
	if launcher.Handler() != nil {
		t.Fatalf("Handler() = %#v, want nil", launcher.Handler())
	}
}

func TestBuildDefaultWorkerRuntimeSystemLauncherWithMemoryUsesWorkerRole(t *testing.T) {
	bootstrap, launcher, err := BuildDefaultWorkerRuntimeSystemLauncherWithMemory(context.Background(), "runtime-test", t.TempDir(), t.TempDir(), nil, clarification.NewManager(4), 100, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildDefaultWorkerRuntimeSystemLauncherWithMemory() error = %v", err)
	}
	if bootstrap.Node.Config.Role != RuntimeNodeRoleWorker {
		t.Fatalf("role = %q, want %q", bootstrap.Node.Config.Role, RuntimeNodeRoleWorker)
	}
	if launcher.Spec().Role != RuntimeNodeRoleWorker {
		t.Fatalf("launcher role = %q, want %q", launcher.Spec().Role, RuntimeNodeRoleWorker)
	}
	if launcher.Handler() == nil {
		t.Fatal("Handler() = nil")
	}
}

func TestBuildDefaultRuntimeSystemLauncherForRoleWithMemoryUsesRequestedRole(t *testing.T) {
	tests := []struct {
		name      string
		role      RuntimeNodeRole
		endpoint  string
		wantServe bool
	}{
		{name: "all-in-one", role: RuntimeNodeRoleAllInOne, wantServe: true},
		{name: "gateway", role: RuntimeNodeRoleGateway, endpoint: "http://worker:8081/dispatch", wantServe: false},
		{name: "worker", role: RuntimeNodeRoleWorker, wantServe: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bootstrap, launcher, err := BuildDefaultRuntimeSystemLauncherForRoleWithMemory(context.Background(), tc.role, "runtime-test", t.TempDir(), tc.endpoint, t.TempDir(), nil, clarification.NewManager(4), 100, nil, nil, nil)
			if err != nil {
				t.Fatalf("BuildDefaultRuntimeSystemLauncherForRoleWithMemory() error = %v", err)
			}
			if bootstrap.Node.Config.Role != tc.role {
				t.Fatalf("role = %q, want %q", bootstrap.Node.Config.Role, tc.role)
			}
			if launcher.Spec().Role != tc.role {
				t.Fatalf("launcher role = %q, want %q", launcher.Spec().Role, tc.role)
			}
			if got := launcher.Handler() != nil; got != tc.wantServe {
				t.Fatalf("launcher serves remote worker = %v, want %v", got, tc.wantServe)
			}
		})
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
