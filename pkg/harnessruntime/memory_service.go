package harnessruntime

import (
	pkgmemory "github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/harness"
)

// MemoryService owns runtime-facing memory construction so compat layers do
// not wire MemoryRuntime directly.
type MemoryService struct {
	runtime *harness.MemoryRuntime
}

func NewMemoryService(store pkgmemory.Storage, extractor pkgmemory.Extractor) *MemoryService {
	return &MemoryService{
		runtime: harness.NewMemoryRuntime(store, extractor),
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
