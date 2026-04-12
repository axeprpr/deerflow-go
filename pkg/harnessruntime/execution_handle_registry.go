package harnessruntime

import (
	"fmt"
	"sync"
)

type ExecutionHandleRegistry interface {
	Register(ExecutionHandle) string
	Resolve(string) (ExecutionHandle, bool)
}

type InMemoryExecutionHandleRegistry struct {
	mu      sync.RWMutex
	nextID  uint64
	handles map[string]ExecutionHandle
}

func NewInMemoryExecutionHandleRegistry() *InMemoryExecutionHandleRegistry {
	return &InMemoryExecutionHandleRegistry{
		handles: make(map[string]ExecutionHandle),
	}
}

func (r *InMemoryExecutionHandleRegistry) Register(handle ExecutionHandle) string {
	if r == nil || handle == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := fmt.Sprintf("exec-%d", r.nextID)
	r.handles[id] = handle
	return id
}

func (r *InMemoryExecutionHandleRegistry) Resolve(id string) (ExecutionHandle, bool) {
	if r == nil || id == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	handle, ok := r.handles[id]
	return handle, ok
}
