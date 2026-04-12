package harnessruntime

import (
	"context"
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

const defaultMemoryUpdateQueueBuffer = 32
const defaultMemoryUpdateTimeout = 30 * time.Second

type MemoryUpdateJob struct {
	Scope    pkgmemory.Scope
	Messages []models.Message
}

type MemoryUpdateExecutor interface {
	Execute(context.Context, MemoryUpdateJob) error
}

type MemoryUpdateQueue interface {
	Enqueue(MemoryUpdateJob)
}

type queueBackedMemoryUpdater struct {
	queue MemoryUpdateQueue
}

func NewQueuedMemoryUpdater(queue MemoryUpdateQueue) harness.MemoryUpdater {
	return queueBackedMemoryUpdater{queue: queue}
}

func (u queueBackedMemoryUpdater) Schedule(scope pkgmemory.Scope, messages []models.Message) {
	if u.queue == nil || len(messages) == 0 {
		return
	}
	u.queue.Enqueue(MemoryUpdateJob{
		Scope:    scope.Normalized(),
		Messages: append([]models.Message(nil), messages...),
	})
}

type RuntimeMemoryUpdateExecutor struct {
	runtime *harness.MemoryRuntime
}

func NewRuntimeMemoryUpdateExecutor(runtime *harness.MemoryRuntime) MemoryUpdateExecutor {
	return RuntimeMemoryUpdateExecutor{runtime: runtime}
}

func (e RuntimeMemoryUpdateExecutor) Execute(ctx context.Context, job MemoryUpdateJob) error {
	if e.runtime == nil || len(job.Messages) == 0 {
		return nil
	}
	return e.runtime.UpdateScope(ctx, job.Scope, job.Messages)
}

type InProcessMemoryUpdateQueue struct {
	executor MemoryUpdateExecutor
	timeout  time.Duration
	jobs     chan MemoryUpdateJob
	wg       sync.WaitGroup
	closeMu  sync.Once
}

func NewInProcessMemoryUpdateQueue(executor MemoryUpdateExecutor, buffer int, timeout time.Duration) *InProcessMemoryUpdateQueue {
	if buffer <= 0 {
		buffer = defaultMemoryUpdateQueueBuffer
	}
	if timeout <= 0 {
		timeout = defaultMemoryUpdateTimeout
	}
	q := &InProcessMemoryUpdateQueue{
		executor: executor,
		timeout:  timeout,
		jobs:     make(chan MemoryUpdateJob, buffer),
	}
	q.wg.Add(1)
	go q.run()
	return q
}

func (q *InProcessMemoryUpdateQueue) Enqueue(job MemoryUpdateJob) {
	if q == nil || q.jobs == nil || len(job.Messages) == 0 {
		return
	}
	q.jobs <- MemoryUpdateJob{
		Scope:    job.Scope.Normalized(),
		Messages: append([]models.Message(nil), job.Messages...),
	}
}

func (q *InProcessMemoryUpdateQueue) Close() error {
	if q == nil {
		return nil
	}
	q.closeMu.Do(func() {
		close(q.jobs)
		q.wg.Wait()
	})
	return nil
}

func (q *InProcessMemoryUpdateQueue) run() {
	defer q.wg.Done()
	for job := range q.jobs {
		if q.executor == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), q.timeout)
		_ = q.executor.Execute(ctx, job)
		cancel()
	}
}
