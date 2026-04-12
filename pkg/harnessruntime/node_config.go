package harnessruntime

import (
	goruntime "runtime"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

// RuntimeNodeConfig describes the in-process runtime node shape. It keeps
// deployment-facing execution choices outside compat protocol code.
type RuntimeNodeConfig struct {
	Sandbox   SandboxManagerConfig
	Transport WorkerTransportConfig
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
		Transport: WorkerTransportConfig{
			Backend: WorkerTransportBackendQueue,
			Buffer:  defaultRunQueueBuffer,
			Workers: workers,
		},
	}
}

func (c RuntimeNodeConfig) BuildSandboxManager() (*SandboxResourceManager, error) {
	return NewSandboxManagerFromConfig(c.Sandbox)
}

func (c RuntimeNodeConfig) BuildDispatcher(runtime DispatchRuntimeConfig) RunDispatcher {
	return NewRuntimeDispatcher(DispatchConfig{
		Topology: transportTopology(c.Transport.Backend),
		Endpoint: c.Transport.Endpoint,
		Buffer:   c.Transport.Buffer,
		Workers:  c.Transport.Workers,
	}, runtime)
}

func transportTopology(backend WorkerTransportBackend) DispatchTopology {
	switch normalizeTransportBackend(backend) {
	case WorkerTransportBackendDirect:
		return DispatchTopologyDirect
	case WorkerTransportBackendRemote:
		return DispatchTopologyRemote
	default:
		return DispatchTopologyQueued
	}
}
