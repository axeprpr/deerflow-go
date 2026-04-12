package harnessruntime

import (
	"github.com/axeprpr/deerflow-go/pkg/harness"
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
)

// MemoryService owns runtime-facing memory construction so compat layers do
// not wire MemoryRuntime directly.
type MemoryService struct {
	runtime *harness.MemoryRuntime
	queue   *InProcessMemoryUpdateQueue
}

func NewMemoryService(store pkgmemory.Storage, extractor pkgmemory.Extractor) *MemoryService {
	runtime := harness.NewMemoryRuntime(store, extractor)
	service := &MemoryService{
		runtime: runtime,
	}
	if runtime != nil && runtime.Enabled() {
		service.queue = NewInProcessMemoryUpdateQueue(NewRuntimeMemoryUpdateExecutor(runtime), 0, 0)
		runtime.SetUpdater(NewQueuedMemoryUpdater(service.queue))
	}
	return &MemoryService{
		runtime: service.runtime,
		queue:   service.queue,
	}
}

func (s *MemoryService) Runtime() *harness.MemoryRuntime {
	if s == nil {
		return nil
	}
	return s.runtime
}

func (s *MemoryService) Enabled() bool {
	return s != nil && s.runtime != nil && s.runtime.Enabled()
}

func (s *MemoryService) Store() pkgmemory.Storage {
	if s == nil || s.runtime == nil {
		return nil
	}
	return s.runtime.Store()
}

func (s *MemoryService) ScopeKey(scope pkgmemory.Scope) string {
	return scope.Key()
}

func (s *MemoryService) Close() error {
	if s == nil || s.queue == nil {
		return nil
	}
	return s.queue.Close()
}
