package langgraphcompat

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestRuntimeProfileBuilderBuildsProfile(t *testing.T) {
	server := &Server{
		clarify: clarification.NewManager(8),
	}
	memoryRuntime := &harness.MemoryRuntime{}

	builder := server.runtimeProfileBuilder(memoryRuntime, nil, nil)
	profile := builder.BuildProfile()

	if profile.RunPolicy == nil {
		t.Fatal("run policy should be configured on runtime profile")
	}
	if server.runtimeExecutionMode() != harnessruntime.ExecutionModeInteractive {
		t.Fatal("compat runtime should default to interactive execution mode")
	}
	if !profile.Features.Clarification.Enabled {
		t.Fatal("clarification feature should be enabled")
	}
	if profile.ToolRuntime != nil || profile.SandboxRuntime != nil {
		t.Fatal("expected nil runtimes when builder receives nil runtime dependencies")
	}
	if profile.Lifecycle == nil {
		t.Fatal("lifecycle profile should not be nil")
	}
}
