package langgraphcompat

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

func (s *Server) createThread(req map[string]any) (map[string]any, int, string) {
	threadID, _ := req["thread_id"].(string)
	if threadID == "" {
		threadID, _ = req["threadId"].(string)
	}
	if threadID == "" {
		threadID = uuid.New().String()
	}
	if err := validateThreadID(threadID); err != nil {
		return nil, 0, err.Error()
	}
	metadata, _ := req["metadata"].(map[string]any)

	session := s.ensureSession(threadID, metadata)
	s.applyThreadValues(session, extractThreadValues(req))
	applyThreadConfigurable(session, req)
	applyThreadStatus(session, req)
	if err := s.persistSessionFile(session); err != nil {
		return nil, http.StatusInternalServerError, "failed to persist thread"
	}
	_ = s.appendThreadHistorySnapshot(threadID)
	return s.threadResponse(session), 0, ""
}

func (s *Server) updateThread(threadID string, req map[string]any) (map[string]any, int, string) {
	s.sessionsMu.Lock()
	session, exists := s.sessions[threadID]
	if !exists {
		s.sessionsMu.Unlock()
		return nil, http.StatusNotFound, "thread not found"
	}

	if metadata, ok := req["metadata"].(map[string]any); ok {
		applyThreadMetadata(session, metadata)
	}
	s.applyThreadValues(session, extractThreadValues(req))
	applyThreadConfigurable(session, req)
	applyThreadStatus(session, req)
	session.UpdatedAt = time.Now().UTC()
	s.sessionsMu.Unlock()

	if err := s.persistSessionFile(session); err != nil {
		return nil, http.StatusInternalServerError, "failed to persist thread"
	}
	_ = s.appendThreadHistorySnapshot(threadID)
	return s.threadResponse(session), 0, ""
}

func (s *Server) deleteThread(threadID string) (int, string) {
	s.sessionsMu.Lock()
	delete(s.sessions, threadID)
	s.sessionsMu.Unlock()
	s.deleteRunsForThread(threadID)
	_ = os.RemoveAll(filepath.Dir(s.threadRoot(threadID)))
	return 0, ""
}
