package harnessruntime

import "github.com/axeprpr/deerflow-go/pkg/harness"

// RuntimeNodeConfig describes the in-process runtime node shape. It keeps
// deployment-facing execution choices outside compat protocol code.
type RuntimeNodeConfig struct {
	Sandbox  SandboxManagerConfig
	Dispatch DispatchConfig
}

func DefaultRuntimeNodeConfig(name, root string) RuntimeNodeConfig {
	return RuntimeNodeConfig{
		Sandbox: SandboxManagerConfig{
			Backend: SandboxBackendLocalLinux,
			Name:    name,
			Root:    root,
			Policy:  harness.FeatureSandboxPolicy{},
		},
		Dispatch: DispatchConfig{
			Topology: DispatchTopologyQueued,
		},
	}
}

func (c RuntimeNodeConfig) BuildSandboxManager() (*SandboxResourceManager, error) {
	return NewSandboxManagerFromConfig(c.Sandbox)
}

func (c RuntimeNodeConfig) BuildDispatcher() RunDispatcher {
	return NewRuntimeDispatcher(c.Dispatch)
}
