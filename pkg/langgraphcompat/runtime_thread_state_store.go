package langgraphcompat

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type compatThreadStateStore struct {
	server *Server
	store  harnessruntime.ThreadStateStore
}

func (s *Server) ensureThreadStateStore() harnessruntime.ThreadStateStore {
	if s == nil {
		return nil
	}
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if s.threadStateStore == nil {
		s.threadStateStore = newCompatThreadStateStore(s)
	}
	return s.threadStateStore
}

func newCompatThreadStateStore(server *Server) harnessruntime.ThreadStateStore {
	var store harnessruntime.ThreadStateStore
	if server != nil {
		store = server.runtimeNode.BuildThreadStateStore()
	}
	if store == nil {
		store = harnessruntime.NewInMemoryThreadStateStore()
	}
	return &compatThreadStateStore{
		server: server,
		store:  store,
	}
}

func newCompatThreadRuntimeState(session *Session) harnessruntime.ThreadRuntimeState {
	if session == nil {
		return harnessruntime.ThreadRuntimeState{}
	}
	state := harnessruntime.ThreadRuntimeState{
		ThreadID:  session.ThreadID,
		Status:    session.Status,
		CreatedAt: firstNonZeroTime(session.CreatedAt, session.UpdatedAt).Format(time.RFC3339Nano),
		UpdatedAt: firstNonZeroTime(session.UpdatedAt, session.CreatedAt).Format(time.RFC3339Nano),
	}
	if len(session.Metadata) > 0 {
		state.Metadata = copyMetadataMap(session.Metadata)
	}
	return state
}

func (s *compatThreadStateStore) LoadThreadRuntimeState(threadID string) (harnessruntime.ThreadRuntimeState, bool) {
	if s == nil || s.store == nil {
		return harnessruntime.ThreadRuntimeState{}, false
	}
	if state, ok := s.store.LoadThreadRuntimeState(threadID); ok {
		return state, true
	}
	if s.server == nil {
		return harnessruntime.ThreadRuntimeState{}, false
	}
	s.server.sessionsMu.RLock()
	session, ok := s.server.sessions[threadID]
	s.server.sessionsMu.RUnlock()
	if !ok {
		return harnessruntime.ThreadRuntimeState{}, false
	}
	state := newCompatThreadRuntimeState(session)
	s.store.SaveThreadRuntimeState(state)
	return state, true
}

func (s *compatThreadStateStore) ListThreadRuntimeStates() []harnessruntime.ThreadRuntimeState {
	if s == nil || s.server == nil {
		return nil
	}
	s.server.sessionsMu.RLock()
	defer s.server.sessionsMu.RUnlock()
	out := make([]harnessruntime.ThreadRuntimeState, 0, len(s.server.sessions))
	for _, session := range s.server.sessions {
		state := newCompatThreadRuntimeState(session)
		out = append(out, state)
		if s.store != nil {
			s.store.SaveThreadRuntimeState(state)
		}
	}
	return out
}

func (s *compatThreadStateStore) SaveThreadRuntimeState(state harnessruntime.ThreadRuntimeState) {
	if s == nil || s.server == nil || s.store == nil {
		return
	}
	s.store.SaveThreadRuntimeState(state)
	session := s.server.ensureSession(state.ThreadID, nil)
	session.Metadata = normalizePersistedThreadMetadata(copyMetadataMap(state.Metadata))
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if _, ok := session.Metadata["thread_id"]; !ok {
		session.Metadata["thread_id"] = state.ThreadID
	}
	session.Status = state.Status
	if state.CreatedAt != "" {
		if createdAt, err := time.Parse(time.RFC3339Nano, state.CreatedAt); err == nil {
			session.CreatedAt = createdAt
		}
	}
	if state.UpdatedAt != "" {
		if updatedAt, err := time.Parse(time.RFC3339Nano, state.UpdatedAt); err == nil {
			session.UpdatedAt = updatedAt
		}
	} else {
		session.UpdatedAt = time.Now().UTC()
	}
	_ = s.server.persistSessionFile(session)
}

func (s *compatThreadStateStore) HasThread(threadID string) bool {
	_, ok := s.LoadThreadRuntimeState(threadID)
	return ok
}

func (s *compatThreadStateStore) MarkThreadStatus(threadID string, status string) {
	state, _ := s.LoadThreadRuntimeState(threadID)
	state.ThreadID = threadID
	state.Status = status
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.SaveThreadRuntimeState(state)
}

func (s *compatThreadStateStore) SetThreadMetadata(threadID string, key string, value any) {
	state, _ := s.LoadThreadRuntimeState(threadID)
	state.ThreadID = threadID
	if state.Metadata == nil {
		state.Metadata = map[string]any{}
	}
	state.Metadata[key] = value
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.SaveThreadRuntimeState(state)
}

func (s *compatThreadStateStore) ClearThreadMetadata(threadID string, key string) {
	state, ok := s.LoadThreadRuntimeState(threadID)
	if !ok {
		return
	}
	delete(state.Metadata, key)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.SaveThreadRuntimeState(state)
}
