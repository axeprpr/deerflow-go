package langgraphcompat

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestRuntimeFeatureBuilderBuildsBundle(t *testing.T) {
	server := &Server{
		clarify: clarification.NewManager(8),
	}
	memoryRuntime := &harness.MemoryRuntime{}

	builder := server.runtimeFeatureBuilder(memoryRuntime)
	bundle := builder.Build()

	if !bundle.Assembly.Clarification.Enabled {
		t.Fatal("clarification feature should be enabled")
	}
	if bundle.Lifecycle == nil {
		t.Fatal("lifecycle bundle should not be nil")
	}
}
