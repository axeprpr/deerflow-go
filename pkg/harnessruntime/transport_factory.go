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
	Remote    RemoteWorkerClient
}

type RemoteWorkerClient interface {
	Submit(context.Context, string, WorkerDispatchEnvelope) ([]byte, error)
}

type remoteWorkerTransport struct {
	endpoint string
	client   RemoteWorkerClient
	results  DispatchResultMarshaler
}

func (t remoteWorkerTransport) Submit(ctx context.Context, env WorkerDispatchEnvelope) (*DispatchResult, error) {
	if t.endpoint == "" {
		return nil, errors.New("remote worker endpoint is required")
	}
	if t.client == nil {
		return nil, errors.New("remote worker client is not configured")
	}
	payload, err := t.client.Submit(ctx, t.endpoint, env)
	if err != nil {
		return nil, err
	}
	results := t.results
	if results == nil {
		results = defaultDispatchResultCodec(nil)
	}
	return results.Decode(payload)
}

func buildWorkerTransport(config WorkerTransportConfig, runtime DispatchRuntimeConfig) WorkerTransport {
	if runtime.Transport != nil {
		return runtime.Transport
	}
	executor := workerExecutorFromConfig(runtime)
	codec := DispatchEnvelopeCodec{Plans: runtime.Codec}

	switch normalizeTransportBackend(config.Backend) {
	case WorkerTransportBackendDirect:
		return NewDirectWorkerTransportWithResults(executor, codec, runtime.Results)
	case WorkerTransportBackendRemote:
		return remoteWorkerTransport{
			endpoint: config.Endpoint,
			client:   runtime.Remote,
			results:  defaultDispatchResultCodec(runtime.Results),
		}
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
