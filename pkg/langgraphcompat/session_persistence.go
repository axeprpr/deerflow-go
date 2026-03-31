package langgraphcompat

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

type persistedSession struct {
	ThreadID  string           `json:"thread_id"`
	Messages  []models.Message `json:"messages"`
	Todos     []Todo           `json:"todos,omitempty"`
	Metadata  map[string]any   `json:"metadata,omitempty"`
	Config    map[string]any   `json:"config,omitempty"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

func (s *Server) loadPersistedSessions() error {
	threadsRoot := filepath.Join(s.dataRoot, "threads")
	entries, err := os.ReadDir(threadsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	sessions := make(map[string]*Session, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		threadID := strings.TrimSpace(entry.Name())
		if threadID == "" {
			continue
		}
		session, err := s.readPersistedSession(threadID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		sessions[threadID] = session
	}

	s.sessionsMu.Lock()
	for threadID, session := range sessions {
		s.sessions[threadID] = session
	}
	s.sessionsMu.Unlock()
	return nil
}

func (s *Server) readPersistedSession(threadID string) (*Session, error) {
	data, err := os.ReadFile(s.sessionStatePath(threadID))
	if err != nil {
		return nil, err
	}

	var stored persistedSession
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	if strings.TrimSpace(stored.ThreadID) == "" {
		stored.ThreadID = threadID
	}
	if stored.Metadata == nil {
		stored.Metadata = map[string]any{}
	}
	if stored.Config == nil {
		stored.Config = defaultThreadConfig(stored.ThreadID)
	}
	if stored.Status == "" {
		stored.Status = "idle"
	}
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}
	if stored.UpdatedAt.IsZero() {
		stored.UpdatedAt = stored.CreatedAt
	}

	return &Session{
		ThreadID:     stored.ThreadID,
		Messages:     append([]models.Message(nil), stored.Messages...),
		Todos:        append([]Todo(nil), stored.Todos...),
		Metadata:     copyMetadataMap(stored.Metadata),
		Configurable: copyMetadataMap(stored.Config),
		Status:       stored.Status,
		PresentFiles: tools.NewPresentFileRegistry(),
		CreatedAt:    stored.CreatedAt,
		UpdatedAt:    stored.UpdatedAt,
	}, nil
}

func (s *Server) persistSessionSnapshot(session *Session) error {
	if session == nil || strings.TrimSpace(session.ThreadID) == "" {
		return nil
	}

	path := s.sessionStatePath(session.ThreadID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	payload := persistedSession{
		ThreadID:  session.ThreadID,
		Messages:  append([]models.Message(nil), session.Messages...),
		Todos:     append([]Todo(nil), session.Todos...),
		Metadata:  copyMetadataMap(session.Metadata),
		Config:    copyMetadataMap(session.Configurable),
		Status:    session.Status,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *Server) deletePersistedSession(threadID string) error {
	err := os.Remove(s.sessionStatePath(threadID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) sessionStatePath(threadID string) string {
	return filepath.Join(s.dataRoot, "threads", threadID, "session.json")
}

func collectArtifactPaths(root string) []string {
	entries := make([]string, 0)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		entries = append(entries, "/mnt/user-data/outputs/"+filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(entries)
	return entries
}

func copyMetadataMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
