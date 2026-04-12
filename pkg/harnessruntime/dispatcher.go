package harnessruntime

import (
	"context"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

type DispatchRequest struct {
	Plan WorkerExecutionPlan
}

type DispatchResult struct {
	Lifecycle *harness.RunState
	Execution *harness.Execution
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
	Topology  DispatchTopology
	Endpoint  string
	Executor  RunExecutor
	Runtime   func() *harness.Runtime
	Specs     WorkerSpecRuntime
	Codec     WorkerPlanMarshaler
	Transport WorkerTransport
	Buffer    int
	Workers   int
}

type transportRunDispatcher struct {
	transport WorkerTransport
	codec     DispatchEnvelopeCodec
}

func NewInProcessRunDispatcher() RunDispatcher {
	return NewRuntimeDispatcher(DispatchConfig{Topology: DispatchTopologyQueued})
}

func NewRuntimeDispatcher(config DispatchConfig) RunDispatcher {
	config.Topology = normalizeDispatchTopology(config)
	codec := DispatchEnvelopeCodec{Plans: config.Codec}
	transport := buildWorkerTransport(config)
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
