package harnessruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type InMemoryThreadStateStore struct {
	mu     sync.RWMutex
	states map[string]ThreadRuntimeState
}

func NewInMemoryThreadStateStore() *InMemoryThreadStateStore {
	return &InMemoryThreadStateStore{states: map[string]ThreadRuntimeState{}}
}

func (s *InMemoryThreadStateStore) LoadThreadRuntimeState(threadID string) (ThreadRuntimeState, bool) {
	if s == nil {
		return ThreadRuntimeState{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[threadID]
	if !ok {
		return ThreadRuntimeState{}, false
	}
	return cloneThreadRuntimeState(state), true
}

func (s *InMemoryThreadStateStore) ListThreadRuntimeStates() []ThreadRuntimeState {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ThreadRuntimeState, 0, len(s.states))
	for _, state := range s.states {
		out = append(out, cloneThreadRuntimeState(state))
	}
	return out
}

func (s *InMemoryThreadStateStore) SaveThreadRuntimeState(state ThreadRuntimeState) {
	if s == nil || strings.TrimSpace(state.ThreadID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.ThreadID] = cloneThreadRuntimeState(state)
}

func (s *InMemoryThreadStateStore) HasThread(threadID string) bool {
	_, ok := s.LoadThreadRuntimeState(threadID)
	return ok
}

func (s *InMemoryThreadStateStore) MarkThreadStatus(threadID string, status string) {
	state, _ := s.LoadThreadRuntimeState(threadID)
	state.ThreadID = strings.TrimSpace(threadID)
	state.Status = strings.TrimSpace(status)
	s.SaveThreadRuntimeState(state)
}

func (s *InMemoryThreadStateStore) SetThreadMetadata(threadID string, key string, value any) {
	state, _ := s.LoadThreadRuntimeState(threadID)
	state.ThreadID = strings.TrimSpace(threadID)
	if state.Metadata == nil {
		state.Metadata = map[string]any{}
	}
	state.Metadata[key] = value
	s.SaveThreadRuntimeState(state)
}

func (s *InMemoryThreadStateStore) ClearThreadMetadata(threadID string, key string) {
	state, ok := s.LoadThreadRuntimeState(threadID)
	if !ok {
		return
	}
	delete(state.Metadata, key)
	s.SaveThreadRuntimeState(state)
}

type JSONFileThreadStateStore struct {
	root string
	mu   sync.Mutex
}

func NewJSONFileThreadStateStore(root string) *JSONFileThreadStateStore {
	return &JSONFileThreadStateStore{root: strings.TrimSpace(root)}
}

func (s *JSONFileThreadStateStore) LoadThreadRuntimeState(threadID string) (ThreadRuntimeState, bool) {
	if s == nil || strings.TrimSpace(threadID) == "" || s.root == "" {
		return ThreadRuntimeState{}, false
	}
	data, err := os.ReadFile(filepath.Join(s.root, threadID+".json"))
	if err != nil {
		return ThreadRuntimeState{}, false
	}
	var state ThreadRuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return ThreadRuntimeState{}, false
	}
	return cloneThreadRuntimeState(state), true
}

func (s *JSONFileThreadStateStore) ListThreadRuntimeStates() []ThreadRuntimeState {
	if s == nil || s.root == "" {
		return nil
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil
	}
	out := make([]ThreadRuntimeState, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		state, ok := s.LoadThreadRuntimeState(strings.TrimSuffix(entry.Name(), ".json"))
		if ok {
			out = append(out, state)
		}
	}
	return out
}

func (s *JSONFileThreadStateStore) SaveThreadRuntimeState(state ThreadRuntimeState) {
	if s == nil || strings.TrimSpace(state.ThreadID) == "" || s.root == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.MkdirAll(s.root, 0o755)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.root, state.ThreadID+".json"), data, 0o644)
}

func (s *JSONFileThreadStateStore) HasThread(threadID string) bool {
	_, ok := s.LoadThreadRuntimeState(threadID)
	return ok
}

func (s *JSONFileThreadStateStore) MarkThreadStatus(threadID string, status string) {
	state, _ := s.LoadThreadRuntimeState(threadID)
	state.ThreadID = strings.TrimSpace(threadID)
	state.Status = strings.TrimSpace(status)
	s.SaveThreadRuntimeState(state)
}

func (s *JSONFileThreadStateStore) SetThreadMetadata(threadID string, key string, value any) {
	state, _ := s.LoadThreadRuntimeState(threadID)
	state.ThreadID = strings.TrimSpace(threadID)
	if state.Metadata == nil {
		state.Metadata = map[string]any{}
	}
	state.Metadata[key] = value
	s.SaveThreadRuntimeState(state)
}

func (s *JSONFileThreadStateStore) ClearThreadMetadata(threadID string, key string) {
	state, ok := s.LoadThreadRuntimeState(threadID)
	if !ok {
		return
	}
	delete(state.Metadata, key)
	s.SaveThreadRuntimeState(state)
}

func cloneThreadRuntimeState(state ThreadRuntimeState) ThreadRuntimeState {
	cloned := ThreadRuntimeState{
		ThreadID:  state.ThreadID,
		Status:    state.Status,
		CreatedAt: state.CreatedAt,
		UpdatedAt: state.UpdatedAt,
	}
	if len(state.Metadata) > 0 {
		cloned.Metadata = make(map[string]any, len(state.Metadata))
		for key, value := range state.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return cloned
}

