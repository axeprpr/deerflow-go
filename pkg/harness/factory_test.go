package harness

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

func TestFactoryAppliesDefaultsAndResolvesSandbox(t *testing.T) {
	tmp := t.TempDir()
	var resolved int
	factory := NewFactory(RuntimeDeps{
		DefaultMaxTurns: 100,
		SandboxProvider: testSandboxProvider(func() (*sandbox.Sandbox, error) {
			resolved++
			return sandbox.New("harness-test", tmp)
		}),
	})

	runAgent, err := factory.NewAgent(AgentRequest{
		Config:   agent.AgentConfig{},
		Features: Features{Sandbox: true},
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	if runAgent == nil {
		t.Fatal("NewAgent() returned nil agent")
	}
	if resolved != 1 {
		t.Fatalf("ResolveSandbox() called %d times, want 1", resolved)
	}
}

func TestFactoryDoesNotResolveSandboxWhenFeatureDisabled(t *testing.T) {
	factory := NewFactory(RuntimeDeps{
		DefaultMaxTurns: 100,
		SandboxProvider: testSandboxProvider(func() (*sandbox.Sandbox, error) {
			t.Fatal("ResolveSandbox should not be called")
			return nil, nil
		}),
	})

	runAgent, err := factory.NewAgent(AgentRequest{
		Config:   agent.AgentConfig{},
		Features: Features{Sandbox: false},
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	if runAgent == nil {
		t.Fatal("NewAgent() returned nil agent")
	}
}

type testSandboxProvider func() (*sandbox.Sandbox, error)

func (p testSandboxProvider) Acquire() (*sandbox.Sandbox, error) { return p() }
func (p testSandboxProvider) Close() error                       { return nil }
