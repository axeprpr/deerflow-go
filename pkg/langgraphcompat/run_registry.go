package langgraphcompat

import (
	"context"
	"strings"
	"sync"
)

type runRegistry struct {
	mu               sync.RWMutex
	streams          map[string]map[uint64]chan StreamEvent
	nextSubscriberID map[string]uint64
	cancels          map[string]context.CancelFunc
}

func newRunRegistry() *runRegistry {
	return &runRegistry{
		streams:          make(map[string]map[uint64]chan StreamEvent),
		nextSubscriberID: make(map[string]uint64),
		cancels:          make(map[string]context.CancelFunc),
	}
}

func (r *runRegistry) setCancel(runID string, cancel context.CancelFunc) {
	if r == nil || cancel == nil || strings.TrimSpace(runID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[runID] = cancel
}

func (r *runRegistry) clearCancel(runID string) {
	if r == nil || strings.TrimSpace(runID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, runID)
}

func (r *runRegistry) cancel(runID string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	cancel := r.cancels[runID]
	r.mu.RUnlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (r *runRegistry) subscribe(runID string, buffer int) (chan StreamEvent, func()) {
	if r == nil || strings.TrimSpace(runID) == "" {
		ch := make(chan StreamEvent)
		close(ch)
		return ch, func() {}
	}
	if buffer <= 0 {
		buffer = 1
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.streams[runID] == nil {
		r.streams[runID] = make(map[uint64]chan StreamEvent)
	}
	r.nextSubscriberID[runID]++
	subID := r.nextSubscriberID[runID]
	ch := make(chan StreamEvent, buffer)
	r.streams[runID][subID] = ch

	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		subscribers := r.streams[runID]
		if subscribers == nil {
			return
		}
		delete(subscribers, subID)
		if len(subscribers) == 0 {
			delete(r.streams, runID)
			delete(r.nextSubscriberID, runID)
		}
	}
}

func (r *runRegistry) publish(runID string, event StreamEvent) {
	if r == nil || strings.TrimSpace(runID) == "" {
		return
	}
	r.mu.RLock()
	subscribers := make([]chan StreamEvent, 0, len(r.streams[runID]))
	for _, subscriber := range r.streams[runID] {
		subscribers = append(subscribers, subscriber)
	}
	r.mu.RUnlock()
	for _, subscriber := range subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (r *runRegistry) subscriberCount(runID string) int {
	if r == nil || strings.TrimSpace(runID) == "" {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.streams[runID])
}

func (r *runRegistry) hasSubscribers(runID string) bool {
	return r.subscriberCount(runID) > 0
}

func (s *Server) ensureRunRegistry() *runRegistry {
	if s == nil {
		return nil
	}
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if s.runRegistry == nil {
		s.runRegistry = newRunRegistry()
	}
	return s.runRegistry
}

func (s *Server) runSubscriberCount(runID string) int {
	return s.ensureRunRegistry().subscriberCount(runID)
}
