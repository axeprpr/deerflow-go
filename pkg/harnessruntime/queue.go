package harnessruntime

import "context"

type WorkerTransport interface {
	Submit(context.Context, WorkerDispatchEnvelope) (*DispatchResult, error)
}

type directWorkerTransport struct {
	executor RunExecutor
	codec    DispatchEnvelopeCodec
}

func NewDirectWorkerTransport(executor RunExecutor, codec DispatchEnvelopeCodec) WorkerTransport {
	return directWorkerTransport{executor: executor, codec: codec}
}

func (t directWorkerTransport) Submit(ctx context.Context, env WorkerDispatchEnvelope) (*DispatchResult, error) {
	executor := t.executor
	if executor == nil {
		executor = NewRuntimeWorker()
	}
	req, err := t.codec.Decode(env)
	if err != nil {
		return nil, err
	}
	return executor.Execute(ctx, req)
}
