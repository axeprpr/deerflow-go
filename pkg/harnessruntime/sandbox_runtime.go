package harnessruntime

import "github.com/axeprpr/deerflow-go/pkg/harness"

// NewLocalSandboxRuntime builds the default runtime-owned sandbox boundary.
// The current implementation still uses the local singleton provider, but the
// enablement and acquisition policy now belongs to the runtime layer.
func NewLocalSandboxRuntime(name, root string) harness.SandboxRuntime {
	runtime, err := NewSandboxRuntimeFromConfig(SandboxManagerConfig{
		Backend: SandboxBackendLocalLinux,
		Name:    name,
		Root:    root,
		Policy:  harness.FeatureSandboxPolicy{},
	})
	if err != nil {
		return nil
	}
	return runtime
}
