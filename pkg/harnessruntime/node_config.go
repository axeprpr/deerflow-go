package harnessruntime

import (
	goruntime "runtime"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

// RuntimeNodeConfig describes the in-process runtime node shape. It keeps
// deployment-facing execution choices outside compat protocol code.
type RuntimeNodeConfig struct {
	Sandbox  SandboxManagerConfig
	Dispatch DispatchConfig
}

func DefaultRuntimeNodeConfig(name, root string) RuntimeNodeConfig {
	workers := goruntime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	return RuntimeNodeConfig{
		Sandbox: SandboxManagerConfig{
			Backend: SandboxBackendLocalLinux,
			Name:    name,
			Root:    root,
			Policy:  harness.FeatureSandboxPolicy{},
		},
		Dispatch: DispatchConfig{
			Topology: DispatchTopologyQueued,
			Buffer:   defaultRunQueueBuffer,
			Workers:  workers,
		},
	}
}

func (c RuntimeNodeConfig) BuildSandboxManager() (*SandboxResourceManager, error) {
	return NewSandboxManagerFromConfig(c.Sandbox)
}

func (c RuntimeNodeConfig) BuildDispatcher() RunDispatcher {
	return NewRuntimeDispatcher(c.Dispatch)
}
