package langgraphcompat

import (
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) threadDataState(threadID string) map[string]any {
	return map[string]any{
		"workspace_path": s.workspaceDir(threadID),
		"uploads_path":   s.uploadsDir(threadID),
		"outputs_path":   s.outputsDir(threadID),
	}
}

func (s *Server) restoredThreadDataState(session *Session) map[string]any {
	if session == nil {
		return map[string]any{}
	}
	current := s.threadDataState(session.ThreadID)
	if threadDataExists(current) {
		return current
	}
	if restored := mapFromMetadata(session.Metadata["thread_data"]); hasThreadWorkspace(restored) {
		return restored
	}
	return current
}

func hasThreadWorkspace(data map[string]any) bool {
	if len(data) == 0 {
		return false
	}
	for _, key := range []string{"workspace_path", "uploads_path", "outputs_path"} {
		if strings.TrimSpace(stringFromAny(data[key])) != "" {
			return true
		}
	}
	return false
}

func threadDataExists(data map[string]any) bool {
	if len(data) == 0 {
		return false
	}
	for _, key := range []string{"workspace_path", "uploads_path", "outputs_path"} {
		path := strings.TrimSpace(stringFromAny(data[key]))
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func (s *Server) workspaceDir(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "workspace")
}

func (s *Server) outputsDir(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "outputs")
}
