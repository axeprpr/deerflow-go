package harnessruntime

import (
	"context"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
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
