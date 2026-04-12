package langgraphcompat

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestRuntimeProfileBuilderBuildsProfile(t *testing.T) {
	server := &Server{
		clarify: clarification.NewManager(8),
	}
	memoryRuntime := &harness.MemoryRuntime{}

	builder := server.runtimeProfileBuilder(memoryRuntime)
	profile := builder.BuildProfile()

	if profile.RunPolicy == nil {
		t.Fatal("run policy should be configured on runtime profile")
	}
	if !profile.Features.Clarification.Enabled {
		t.Fatal("clarification feature should be enabled")
	}
	if profile.Lifecycle == nil {
		t.Fatal("lifecycle profile should not be nil")
	}
}
