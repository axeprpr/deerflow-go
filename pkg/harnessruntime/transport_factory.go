package harnessruntime

import (
	"context"
	"errors"
)

type WorkerTransportBackend string

const (
	WorkerTransportBackendDirect WorkerTransportBackend = "direct"
	WorkerTransportBackendQueue  WorkerTransportBackend = "queue"
	WorkerTransportBackendRemote WorkerTransportBackend = "remote"
)

type remoteWorkerTransport struct {
	endpoint string
}

func (t remoteWorkerTransport) Submit(context.Context, WorkerDispatchEnvelope) (*DispatchResult, error) {
	if t.endpoint == "" {
		return nil, errors.New("remote worker endpoint is required")
	}
	return nil, errors.New("remote worker transport not implemented")
}

func buildWorkerTransport(config DispatchConfig) WorkerTransport {
	if config.Transport != nil {
		return config.Transport
	}
	executor := workerExecutorFromConfig(config)
	codec := DispatchEnvelopeCodec{Plans: config.Codec}

	switch config.Topology {
	case DispatchTopologyDirect:
		return NewDirectWorkerTransport(executor, codec)
	case DispatchTopologyRemote:
		return remoteWorkerTransport{endpoint: config.Endpoint}
	default:
		return NewInProcessRunQueueWithCodec(executor, config.Buffer, config.Workers, config.Codec)
	}
}

func normalizeDispatchTopology(config DispatchConfig) DispatchTopology {
	switch config.Topology {
	case DispatchTopologyDirect, DispatchTopologyQueued, DispatchTopologyRemote:
		return config.Topology
	default:
		return DispatchTopologyQueued
	}
}

func workerExecutorFromConfig(config DispatchConfig) RunExecutor {
	if config.Executor != nil {
		return config.Executor
	}
	if config.Runtime != nil {
		return NewRuntimeWorkerSource(config.Runtime, config.Specs)
	}
	return NewRuntimeWorker()
}
