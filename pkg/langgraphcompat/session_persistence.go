package langgraphcompat

import (
	"bufio"
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
	"github.com/google/uuid"
)

type persistedSession struct {
	ThreadID  string           `json:"thread_id"`
	Messages  []models.Message `json:"messages"`
	Todos     []Todo           `json:"todos,omitempty"`
	Values    map[string]any   `json:"values,omitempty"`
	Metadata  map[string]any   `json:"metadata,omitempty"`
	Config    map[string]any   `json:"config,omitempty"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type persistedHistoryEntry struct {
	CheckpointID string `json:"checkpoint_id"`
	persistedSession
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
		Values:       copyMetadataMap(stored.Values),
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
		Values:    copyMetadataMap(session.Values),
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
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return s.appendPersistedHistory(session)
}

func (s *Server) deletePersistedSession(threadID string) error {
	err := os.Remove(s.sessionStatePath(threadID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	err = os.Remove(s.sessionHistoryPath(threadID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) sessionStatePath(threadID string) string {
	return filepath.Join(s.dataRoot, "threads", threadID, "session.json")
}

func (s *Server) sessionHistoryPath(threadID string) string {
	return filepath.Join(s.dataRoot, "threads", threadID, "history.jsonl")
}

func (s *Server) appendPersistedHistory(session *Session) error {
	entry := persistedHistoryEntry{
		CheckpointID: uuid.New().String(),
		persistedSession: persistedSession{
			ThreadID:  session.ThreadID,
			Messages:  append([]models.Message(nil), session.Messages...),
			Todos:     append([]Todo(nil), session.Todos...),
			Values:    copyMetadataMap(session.Values),
			Metadata:  copyMetadataMap(session.Metadata),
			Config:    copyMetadataMap(session.Configurable),
			Status:    session.Status,
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
		},
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	path := s.sessionHistoryPath(session.ThreadID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *Server) readPersistedHistory(threadID string) ([]persistedHistoryEntry, error) {
	f, err := os.Open(s.sessionHistoryPath(threadID))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	entries := make([]persistedHistoryEntry, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry persistedHistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, err
		}
		if strings.TrimSpace(entry.ThreadID) == "" {
			entry.ThreadID = threadID
		}
		if strings.TrimSpace(entry.CheckpointID) == "" {
			entry.CheckpointID = uuid.New().String()
		}
		if entry.Metadata == nil {
			entry.Metadata = map[string]any{}
		}
		if entry.Config == nil {
			entry.Config = defaultThreadConfig(entry.ThreadID)
		}
		if entry.Status == "" {
			entry.Status = "idle"
		}
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = entry.UpdatedAt
		}
		if entry.UpdatedAt.IsZero() {
			entry.UpdatedAt = entry.CreatedAt
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func collectArtifactPaths(root string, virtualPrefix string) []string {
	type artifactEntry struct {
		path     string
		modified time.Time
	}

	virtualPrefix = "/" + strings.Trim(strings.TrimSpace(virtualPrefix), "/")
	if virtualPrefix == "/" {
		virtualPrefix = ""
	}

	entries := make([]artifactEntry, 0)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		entries = append(entries, artifactEntry{
			path:     virtualPrefix + "/" + filepath.ToSlash(rel),
			modified: info.ModTime(),
		})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].modified.Equal(entries[j].modified) {
			return entries[i].path < entries[j].path
		}
		return entries[i].modified.After(entries[j].modified)
	})

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.path)
	}
	return paths
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
