package harnessruntime

import (
	"context"
	"errors"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type WorkerTransportBackend string

const (
	WorkerTransportBackendDirect WorkerTransportBackend = "direct"
	WorkerTransportBackendQueue  WorkerTransportBackend = "queue"
	WorkerTransportBackendRemote WorkerTransportBackend = "remote"
)

type WorkerTransportConfig struct {
	Backend  WorkerTransportBackend
	Endpoint string
	Buffer   int
	Workers  int
}

type DispatchRuntimeConfig struct {
	Executor  RunExecutor
	Runtime   func() *harness.Runtime
	Specs     WorkerSpecRuntime
	Codec     WorkerPlanMarshaler
	Results   DispatchResultMarshaler
	Transport WorkerTransport
}

type remoteWorkerTransport struct {
	endpoint string
}

func (t remoteWorkerTransport) Submit(context.Context, WorkerDispatchEnvelope) (*DispatchResult, error) {
	if t.endpoint == "" {
		return nil, errors.New("remote worker endpoint is required")
	}
	return nil, errors.New("remote worker transport not implemented")
}

func buildWorkerTransport(config WorkerTransportConfig, runtime DispatchRuntimeConfig) WorkerTransport {
	if runtime.Transport != nil {
		return runtime.Transport
	}
	executor := workerExecutorFromConfig(runtime)
	codec := DispatchEnvelopeCodec{Plans: runtime.Codec}

	switch normalizeTransportBackend(config.Backend) {
	case WorkerTransportBackendDirect:
		return NewDirectWorkerTransport(executor, codec)
	case WorkerTransportBackendRemote:
		return remoteWorkerTransport{endpoint: config.Endpoint}
	default:
		return NewInProcessRunQueueWithCodec(executor, config.Buffer, config.Workers, runtime.Codec, runtime.Results)
	}
}

func normalizeTransportBackend(backend WorkerTransportBackend) WorkerTransportBackend {
	switch backend {
	case WorkerTransportBackendDirect, WorkerTransportBackendQueue, WorkerTransportBackendRemote:
		return backend
	default:
		return WorkerTransportBackendQueue
	}
}

func workerExecutorFromConfig(config DispatchRuntimeConfig) RunExecutor {
	if config.Executor != nil {
		return config.Executor
	}
	if config.Runtime != nil {
		return NewRuntimeWorkerSource(config.Runtime, config.Specs)
	}
	return NewRuntimeWorker()
}
