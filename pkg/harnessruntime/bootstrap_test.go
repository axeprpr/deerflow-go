package harnessruntime

import (
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
