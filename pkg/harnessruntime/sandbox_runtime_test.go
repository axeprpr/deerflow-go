package harnessruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestNewLocalSandboxRuntimeDefersSandboxCreationUntilEnabled(t *testing.T) {
	tmp := t.TempDir()
	runtime := NewLocalSandboxRuntime("runtime-test", tmp)

	sandboxDir := filepath.Join(tmp, "runtime-test")
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Fatalf("sandbox dir exists before resolve: err=%v", err)
	}

	sb, err := runtime.Resolve(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: false}})
	if err != nil {
		t.Fatalf("Resolve(disabled) error = %v", err)
	}
	if sb != nil {
		t.Fatalf("Resolve(disabled) returned sandbox = %#v, want nil", sb)
	}
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Fatalf("sandbox dir exists after disabled resolve: err=%v", err)
	}

	sb, err = runtime.Resolve(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("Resolve(enabled) error = %v", err)
	}
	if sb == nil {
		t.Fatal("Resolve(enabled) returned nil sandbox")
	}
	if _, err := os.Stat(sandboxDir); err != nil {
		t.Fatalf("sandbox dir missing after enabled resolve: %v", err)
	}
}
