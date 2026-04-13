package harness

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/sandbox"
)

func TestFactoryAppliesDefaultsAndResolvesSandbox(t *testing.T) {
	tmp := t.TempDir()
	var resolved int
	factory := NewFactory(RuntimeDeps{
		DefaultMaxTurns: 100,
		SandboxRuntime: NewStaticSandboxRuntime(
			testSandboxProvider(func() (sandbox.Session, error) {
				resolved++
				return sandbox.New("harness-test", tmp)
			}),
			FeatureSandboxPolicy{},
		),
	})

	runAgent, err := factory.NewAgent(AgentRequest{
		Spec:     AgentSpec{},
		Features: FeatureSet{Sandbox: true},
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
		SandboxRuntime: NewStaticSandboxRuntime(
			testSandboxProvider(func() (sandbox.Session, error) {
				t.Fatal("ResolveSandbox should not be called")
				return nil, nil
			}),
			FeatureSandboxPolicy{},
		),
	})

	runAgent, err := factory.NewAgent(AgentRequest{
		Spec:     AgentSpec{},
		Features: FeatureSet{Sandbox: false},
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	if runAgent == nil {
		t.Fatal("NewAgent() returned nil agent")
	}
}

func TestRuntimeUsesProfileResolverForPerRequestExecutionMode(t *testing.T) {
	tmp := t.TempDir()
	var resolved int

	runtime := NewRuntime(RuntimeDeps{
		ProfileResolver: testProfileResolver{root: tmp, resolved: &resolved},
	}, nil)

	runAgent, err := runtime.NewAgent(AgentRequest{
		Spec:     AgentSpec{ExecutionMode: "background"},
		Features: FeatureSet{Sandbox: true},
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	if runAgent == nil {
		t.Fatal("NewAgent() returned nil agent")
	}
	if resolved != 1 {
		t.Fatalf("resolved=%d want=1", resolved)
	}
}

type testProfileResolver struct {
	root     string
	resolved *int
}

func (r testProfileResolver) ResolveProfile(base RuntimeProfile, req AgentRequest) RuntimeProfile {
	if req.Spec.ExecutionMode != "background" {
		return base
	}
	base.SandboxRuntime = NewStaticSandboxRuntime(
		testSandboxProvider(func() (sandbox.Session, error) {
			if r.resolved != nil {
				(*r.resolved)++
			}
			return sandbox.New("harness-profile-resolver", r.root)
		}),
		FeatureSandboxPolicy{},
	)
	return base
}

type testSandboxProvider func() (sandbox.Session, error)

func (p testSandboxProvider) Acquire() (sandbox.Session, error) { return p() }
func (p testSandboxProvider) Close() error                      { return nil }
