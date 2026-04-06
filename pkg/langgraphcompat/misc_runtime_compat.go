package langgraphcompat

import (
	"mime"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

var suggestionBulletRE = regexp.MustCompile(`^(?:[-*+]\s+|\d+[.)]\s+)`)

const (
	generalPurposeSubagentPrompt = "You are a general-purpose subagent. Execute delegated work directly, report concrete outcomes, and avoid unnecessary meta commentary."
	bashSubagentPrompt           = "You are a bash-focused subagent. Prefer shell commands, keep filesystem changes minimal, and report concrete command outcomes."
)

func (s *Server) setThreadValue(threadID, key string, value any) {
	threadID = strings.TrimSpace(threadID)
	key = strings.TrimSpace(key)
	if threadID == "" || key == "" || s == nil {
		return
	}

	s.sessionsMu.Lock()
	session := s.sessions[threadID]
	if session == nil {
		session = &Session{
			ThreadID:     threadID,
			Messages:     []models.Message{},
			Todos:        nil,
			Values:       map[string]any{},
			Metadata:     map[string]any{},
			Configurable: defaultThreadConfig(threadID),
			Status:       "idle",
			PresentFiles: tools.NewPresentFileRegistry(),
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		s.sessions[threadID] = session
	}
	if session.Values == nil {
		session.Values = map[string]any{}
	}
	session.Values[key] = value
	session.UpdatedAt = time.Now().UTC()
	snapshot := cloneSession(session)
	s.sessionsMu.Unlock()

	_ = s.persistSessionSnapshot(snapshot)
}

func cloneSession(in *Session) *Session {
	if in == nil {
		return nil
	}
	out := *in
	out.Messages = append([]models.Message(nil), in.Messages...)
	out.Todos = append([]Todo(nil), in.Todos...)
	out.Values = copyMetadataMap(in.Values)
	out.Metadata = copyMetadataMap(in.Metadata)
	out.Configurable = copyMetadataMap(in.Configurable)
	if in.PresentFiles != nil {
		out.PresentFiles = in.PresentFiles
	}
	return &out
}

func isTransientTodoReminderMessage(msg models.Message) bool {
	if msg.Role != models.RoleHuman {
		return false
	}
	if msg.Metadata == nil {
		return false
	}
	value, _ := msg.Metadata[transientTodoReminderMetadataKey]
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func contentDisposition(disposition, filename string) string {
	disposition = strings.TrimSpace(disposition)
	if disposition == "" {
		disposition = "attachment"
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" {
		filename = "download"
	}
	return mime.FormatMediaType(disposition, map[string]string{"filename": filename})
}

func (s *Server) listUploadedFiles(threadID string) []map[string]any {
	if s == nil || strings.TrimSpace(threadID) == "" {
		return nil
	}
	return s.uploadedFilesState(threadID)
}

func deriveMemorySessionID(threadID, agentName string) string {
	threadID = strings.TrimSpace(threadID)
	agentName = strings.TrimSpace(agentName)
	if agentName != "" {
		return "agent:" + strings.ToLower(agentName)
	}
	return threadID
}
