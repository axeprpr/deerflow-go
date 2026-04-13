package harnessruntime

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type InMemoryRunEventStore struct {
	mu               sync.RWMutex
	events           map[string][]RunEvent
	streams          map[string]map[uint64]chan RunEvent
	nextSubscriberID map[string]uint64
}

func NewInMemoryRunEventStore() *InMemoryRunEventStore {
	return &InMemoryRunEventStore{
		events:           map[string][]RunEvent{},
		streams:          map[string]map[uint64]chan RunEvent{},
		nextSubscriberID: map[string]uint64{},
	}
}

func (s *InMemoryRunEventStore) NextRunEventIndex(runID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events[runID]) + 1
}

func (s *InMemoryRunEventStore) AppendRunEvent(runID string, event RunEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.events[runID] = append(s.events[runID], event)
	subscribers := make([]chan RunEvent, 0, len(s.streams[runID]))
	for _, subscriber := range s.streams[runID] {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.Unlock()
	for _, subscriber := range subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (s *InMemoryRunEventStore) LoadRunEvents(runID string) []RunEvent {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]RunEvent(nil), s.events[runID]...)
}

func (s *InMemoryRunEventStore) ReplaceRunEvents(runID string, events []RunEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[runID] = append([]RunEvent(nil), events...)
}

func (s *InMemoryRunEventStore) SubscribeRunEvents(runID string, buffer int) (<-chan RunEvent, func()) {
	if s == nil {
		ch := make(chan RunEvent)
		close(ch)
		return ch, func() {}
	}
	if buffer <= 0 {
		buffer = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streams[runID] == nil {
		s.streams[runID] = map[uint64]chan RunEvent{}
	}
	s.nextSubscriberID[runID]++
	id := s.nextSubscriberID[runID]
	ch := make(chan RunEvent, buffer)
	s.streams[runID][id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if streams := s.streams[runID]; streams != nil {
			delete(streams, id)
			if len(streams) == 0 {
				delete(s.streams, runID)
				delete(s.nextSubscriberID, runID)
			}
		}
		close(ch)
	}
}

type JSONFileRunEventStore struct {
	root  string
	mu    sync.Mutex
	codec RunEventLogMarshaler
}

func NewJSONFileRunEventStore(root string) *JSONFileRunEventStore {
	return &JSONFileRunEventStore{
		root:  strings.TrimSpace(root),
		codec: defaultRunEventLogCodec(nil),
	}
}

func (s *JSONFileRunEventStore) NextRunEventIndex(runID string) int {
	return len(s.LoadRunEvents(runID)) + 1
}

func (s *JSONFileRunEventStore) AppendRunEvent(runID string, event RunEvent) {
	events := s.LoadRunEvents(runID)
	events = append(events, event)
	s.ReplaceRunEvents(runID, events)
}

func (s *JSONFileRunEventStore) LoadRunEvents(runID string) []RunEvent {
	if s == nil || strings.TrimSpace(runID) == "" || s.root == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.root, runID+".json"))
	if err != nil {
		return nil
	}
	codec := defaultRunEventLogCodec(s.codec)
	events, err := codec.Decode(data)
	if err != nil {
		return nil
	}
	return append([]RunEvent(nil), events...)
}

func (s *JSONFileRunEventStore) ReplaceRunEvents(runID string, events []RunEvent) {
	if s == nil || strings.TrimSpace(runID) == "" || s.root == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.MkdirAll(s.root, 0o755)
	codec := defaultRunEventLogCodec(s.codec)
	data, err := codec.Encode(events)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.root, runID+".json"), data, 0o644)
}

func (s *JSONFileRunEventStore) SubscribeRunEvents(runID string, buffer int) (<-chan RunEvent, func()) {
	return newPollingRunEventSubscription(buffer, func() []RunEvent {
		return s.LoadRunEvents(runID)
	})
}

func newPollingRunEventSubscription(buffer int, load func() []RunEvent) (<-chan RunEvent, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	if load == nil {
		ch := make(chan RunEvent)
		close(ch)
		return ch, func() {}
	}
	out := make(chan RunEvent, buffer)
	done := make(chan struct{})
	go func() {
		defer close(out)
		last := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			events := load()
			if last > len(events) {
				last = 0
			}
			for _, event := range events[last:] {
				select {
				case out <- event:
				case <-done:
					return
				}
			}
			last = len(events)
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	return out, func() { close(done) }
}
