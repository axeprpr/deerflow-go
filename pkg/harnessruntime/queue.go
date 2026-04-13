package harnessruntime

import "context"

type WorkerTransport interface {
	Submit(context.Context, WorkerDispatchEnvelope) (*DispatchResult, error)
}

type directWorkerTransport struct {
	executor RunExecutor
	codec    DispatchEnvelopeCodec
	results  DispatchResultMarshaler
}

func NewDirectWorkerTransport(executor RunExecutor, codec DispatchEnvelopeCodec) WorkerTransport {
	return NewDirectWorkerTransportWithResults(executor, codec, nil)
}

func NewDirectWorkerTransportWithResults(executor RunExecutor, codec DispatchEnvelopeCodec, results DispatchResultMarshaler) WorkerTransport {
	return directWorkerTransport{
		executor: executor,
		codec:    codec,
		results:  defaultDispatchResultCodec(results),
	}
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
	result, err := executor.Execute(ctx, req)
	if err != nil {
		return nil, err
	}
	results := t.results
	if results == nil {
		results = defaultDispatchResultCodec(nil)
	}
	payload, err := results.Encode(result)
	if err != nil {
		return nil, err
	}
	return results.Decode(payload)
}
