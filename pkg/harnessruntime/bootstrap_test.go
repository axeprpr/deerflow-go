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
