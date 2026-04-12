package harnessruntime

import (
	"context"
	"sync"
)

const defaultRunQueueBuffer = 32

type dispatchJob struct {
	ctx  context.Context
	req  DispatchRequest
	resp chan dispatchResult
}

type dispatchResult struct {
	result *DispatchResult
	err    error
}

// InProcessRunQueue provides a real queue/worker seam while staying cheap for
// single-node mode. Future distributed topologies can replace this queue
// without changing coordinator call sites.
type InProcessRunQueue struct {
	executor RunExecutor
	jobs     chan dispatchJob
	wg       sync.WaitGroup
	closeMu  sync.Once
}

func NewInProcessRunQueue(executor RunExecutor, buffer int) *InProcessRunQueue {
	if buffer <= 0 {
		buffer = defaultRunQueueBuffer
	}
	if executor == nil {
		executor = NewRuntimeWorker()
	}
	q := &InProcessRunQueue{
		executor: executor,
		jobs:     make(chan dispatchJob, buffer),
	}
	q.wg.Add(1)
	go q.run()
	return q
}

func (q *InProcessRunQueue) Submit(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	if q == nil || q.jobs == nil {
		return NewRuntimeWorker().Execute(ctx, req)
	}
	resp := make(chan dispatchResult, 1)
	job := dispatchJob{
		ctx:  ctx,
		req:  req,
		resp: resp,
	}
	select {
	case q.jobs <- job:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-resp:
		return result.result, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *InProcessRunQueue) Close() error {
	if q == nil {
		return nil
	}
	q.closeMu.Do(func() {
		close(q.jobs)
		q.wg.Wait()
	})
	return nil
}

func (q *InProcessRunQueue) run() {
	defer q.wg.Done()
	for job := range q.jobs {
		result, err := q.executor.Execute(job.ctx, job.req)
		job.resp <- dispatchResult{result: result, err: err}
		close(job.resp)
	}
}
