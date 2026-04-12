package harnessruntime

import "sync"

type InMemoryRunStore struct {
	mu               sync.RWMutex
	snapshots        map[string]RunSnapshot
	streams          map[string]map[uint64]chan RunEvent
	nextSubscriberID map[string]uint64
}

func NewInMemoryRunStore() *InMemoryRunStore {
	return &InMemoryRunStore{
		snapshots:        map[string]RunSnapshot{},
		streams:          map[string]map[uint64]chan RunEvent{},
		nextSubscriberID: map[string]uint64{},
	}
}

func (s *InMemoryRunStore) LoadRunSnapshot(runID string) (RunSnapshot, bool) {
	if s == nil {
		return RunSnapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[runID]
	if !ok {
		return RunSnapshot{}, false
	}
	return cloneRunSnapshot(snapshot), true
}

func (s *InMemoryRunStore) ListRunSnapshots(threadID string) []RunSnapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RunSnapshot, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		if threadID != "" && snapshot.Record.ThreadID != threadID {
			continue
		}
		out = append(out, cloneRunSnapshot(snapshot))
	}
	return out
}

func (s *InMemoryRunStore) SaveRunSnapshot(snapshot RunSnapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snapshot.Record.RunID] = cloneRunSnapshot(snapshot)
}

func (s *InMemoryRunStore) NextRunEventIndex(runID string) int {
	snapshot, ok := s.LoadRunSnapshot(runID)
	if !ok {
		return 1
	}
	return len(snapshot.Events) + 1
}

func (s *InMemoryRunStore) AppendRunEvent(runID string, event RunEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	snapshot := s.snapshots[runID]
	snapshot.Events = append(snapshot.Events, event)
	s.snapshots[runID] = cloneRunSnapshot(snapshot)
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

func (s *InMemoryRunStore) LoadRunEvents(runID string) []RunEvent {
	snapshot, ok := s.LoadRunSnapshot(runID)
	if !ok {
		return nil
	}
	return append([]RunEvent(nil), snapshot.Events...)
}

func (s *InMemoryRunStore) SubscribeRunEvents(runID string, buffer int) (<-chan RunEvent, func()) {
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

func cloneRunSnapshot(snapshot RunSnapshot) RunSnapshot {
	return RunSnapshot{
		Record: snapshot.Record,
		Events: append([]RunEvent(nil), snapshot.Events...),
	}
}

