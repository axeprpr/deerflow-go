package langgraphcompat

import "github.com/axeprpr/deerflow-go/pkg/harnessruntime"

type localThreadStateStore struct {
	server *Server
}

func (s *Server) ensureThreadStateStore() harnessruntime.ThreadStateStore {
	if s == nil {
		return nil
	}
	return localThreadStateStore{server: s}
}

func (s localThreadStateStore) HasThread(threadID string) bool {
	if s.server == nil {
		return false
	}
	s.server.sessionsMu.RLock()
	defer s.server.sessionsMu.RUnlock()
	_, exists := s.server.sessions[threadID]
	return exists
}

func (s localThreadStateStore) MarkThreadStatus(threadID string, status string) {
	if s.server == nil {
		return
	}
	s.server.markThreadStatus(threadID, status)
}

func (s localThreadStateStore) SetThreadMetadata(threadID string, key string, value any) {
	if s.server == nil {
		return
	}
	s.server.setThreadMetadata(threadID, key, value)
}

func (s localThreadStateStore) ClearThreadMetadata(threadID string, key string) {
	if s.server == nil {
		return
	}
	s.server.deleteThreadMetadata(threadID, key)
}

