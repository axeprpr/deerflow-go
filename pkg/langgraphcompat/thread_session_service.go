package langgraphcompat

import (
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/google/uuid"
)

func (s *Server) ensureSession(threadID string, metadata map[string]any) *Session {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if session, exists := s.sessions[threadID]; exists {
		if metadata != nil {
			applyThreadMetadata(session, metadata)
		}
		if session.Values == nil {
			session.Values = map[string]any{}
		}
		if session.Configurable == nil {
			session.Configurable = defaultThreadConfig(threadID)
		}
		return session
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata = normalizePersistedThreadMetadata(metadata)
	if _, exists := metadata["thread_id"]; !exists {
		metadata["thread_id"] = threadID
	}
	checkpointID := firstNonEmpty(stringValue(metadata["checkpoint_id"]), stringValue(metadata["checkpointId"]))
	if checkpointID == "" && len(metadata) > 1 {
		checkpointID = uuid.New().String()
	}
	if checkpointID != "" {
		metadata["checkpoint_id"] = checkpointID
		if _, exists := metadata["checkpoint_thread_id"]; !exists {
			metadata["checkpoint_thread_id"] = threadID
		}
	}
	now := time.Now().UTC()
	session := &Session{
		CheckpointID: checkpointID,
		ThreadID:     threadID,
		Messages:     []models.Message{},
		Values:       map[string]any{},
		Metadata:     metadata,
		Configurable: defaultThreadConfig(threadID),
		Status:       "idle",
		PresentFiles: tools.NewPresentFileRegistry(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.sessions[threadID] = session
	_ = s.persistSessionFile(session)
	return session
}

func threadStateRequestProvidesCheckpoint(req map[string]any) bool {
	metadata, _ := req["metadata"].(map[string]any)
	if len(metadata) == 0 {
		return false
	}
	for _, key := range []string{
		"checkpoint_id",
		"checkpointId",
		"parent_checkpoint_id",
		"parentCheckpointId",
		"checkpoint_ns",
		"checkpointNs",
		"parent_checkpoint_ns",
		"parentCheckpointNs",
		"checkpoint_thread_id",
		"checkpointThreadId",
		"parent_checkpoint_thread_id",
		"parentCheckpointThreadId",
		"checkpoint",
		"checkpointObject",
		"parent_checkpoint",
		"parentCheckpoint",
	} {
		if _, ok := metadata[key]; ok {
			return true
		}
	}
	return false
}

func clearSessionCheckpoint(session *Session) {
	if session == nil {
		return
	}
	session.CheckpointID = ""
	if session.Metadata == nil {
		return
	}
	delete(session.Metadata, "checkpoint_id")
	delete(session.Metadata, "checkpoint_ns")
	delete(session.Metadata, "checkpoint_thread_id")
}

func (s *Server) markThreadStatus(threadID string, status string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if session, exists := s.sessions[threadID]; exists {
		session.Status = status
		session.UpdatedAt = time.Now().UTC()
		_ = s.persistSessionFile(session)
	}
}

func (s *Server) setThreadMetadata(threadID string, key string, value any) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if session, exists := s.sessions[threadID]; exists {
		if session.Metadata == nil {
			session.Metadata = make(map[string]any)
		}
		session.Metadata[key] = value
		session.UpdatedAt = time.Now().UTC()
		_ = s.persistSessionFile(session)
	}
}

func (s *Server) deleteThreadMetadata(threadID string, key string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if session, exists := s.sessions[threadID]; exists {
		if session.Metadata != nil {
			delete(session.Metadata, key)
		}
		session.UpdatedAt = time.Now().UTC()
		_ = s.persistSessionFile(session)
	}
}
