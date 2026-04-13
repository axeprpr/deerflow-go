package harnessruntime

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type DispatchRequest struct {
	Plan WorkerExecutionPlan
}

type DispatchResult struct {
	Lifecycle *harness.RunState
	Handle    ExecutionHandle
	Execution ExecutionDescriptor
	Completed *agent.RunResult
}

type RunDispatcher interface {
	Dispatch(context.Context, DispatchRequest) (*DispatchResult, error)
}

type DispatchTopology string

const (
	DispatchTopologyDirect DispatchTopology = "direct"
	DispatchTopologyQueued DispatchTopology = "queued"
	DispatchTopologyRemote DispatchTopology = "remote"
)

type DispatchConfig struct {
	Topology DispatchTopology
	Endpoint string
	Buffer   int
	Workers  int
}

type transportRunDispatcher struct {
	transport WorkerTransport
	codec     DispatchEnvelopeCodec
}

func NewInProcessRunDispatcher() RunDispatcher {
	return NewRuntimeDispatcher(DispatchConfig{Topology: DispatchTopologyQueued}, DispatchRuntimeConfig{})
}

func NewRuntimeDispatcher(config DispatchConfig, runtime DispatchRuntimeConfig) RunDispatcher {
	codec := DispatchEnvelopeCodec{Plans: runtime.Codec}
	transport := buildWorkerTransport(WorkerTransportConfig{
		Backend:  config.transportBackend(),
		Endpoint: config.Endpoint,
		Buffer:   config.Buffer,
		Workers:  config.Workers,
	}, runtime)
	return transportRunDispatcher{transport: transport, codec: codec}
}

func NewQueuedRunDispatcher(transport WorkerTransport) RunDispatcher {
	return transportRunDispatcher{transport: transport, codec: DispatchEnvelopeCodec{}}
}

func (d transportRunDispatcher) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	transport := d.transport
	if transport == nil {
		transport = NewDirectWorkerTransport(nil, d.codec)
	}
	envelope, err := d.codec.Encode(req)
	if err != nil {
		return nil, err
	}
	return transport.Submit(ctx, envelope)
}

func (c DispatchConfig) transportBackend() WorkerTransportBackend {
	switch c.Topology {
	case DispatchTopologyDirect:
		return WorkerTransportBackendDirect
	case DispatchTopologyRemote:
		return WorkerTransportBackendRemote
	default:
		return WorkerTransportBackendQueue
	}
}
