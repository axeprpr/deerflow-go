package harness

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

func TestStaticSandboxRuntimeHonorsFeaturePolicy(t *testing.T) {
	var acquired int
	runtime := NewStaticSandboxRuntime(
		testSandboxProvider(func() (sandbox.Session, error) {
			acquired++
			return &sandbox.Sandbox{}, nil
		}),
		FeatureSandboxPolicy{},
	)

	sb, err := runtime.Resolve(AgentRequest{Features: FeatureSet{Sandbox: false}})
	if err != nil {
		t.Fatalf("Resolve(disabled) error = %v", err)
	}
	if sb != nil {
		t.Fatalf("Resolve(disabled) returned sandbox = %#v, want nil", sb)
	}
	if acquired != 0 {
		t.Fatalf("Acquire() calls = %d, want 0", acquired)
	}

	sb, err = runtime.Resolve(AgentRequest{Features: FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("Resolve(enabled) error = %v", err)
	}
	if sb == nil {
		t.Fatal("Resolve(enabled) returned nil sandbox")
	}
	if acquired != 1 {
		t.Fatalf("Acquire() calls = %d, want 1", acquired)
	}
}
