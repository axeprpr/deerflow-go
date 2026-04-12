package langgraphcompat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type persistedThreadSession struct {
	ThreadID  string           `json:"thread_id"`
	Messages  []models.Message `json:"messages"`
	Metadata  map[string]any   `json:"metadata"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type persistedThreadRun struct {
	RunID       string                    `json:"run_id"`
	ThreadID    string                    `json:"thread_id"`
	AssistantID string                    `json:"assistant_id"`
	Status      string                    `json:"status"`
	CreatedAt   time.Time                 `json:"created_at"`
	UpdatedAt   time.Time                 `json:"updated_at"`
	Events      []harnessruntime.RunEvent `json:"events"`
	Error       string                    `json:"error,omitempty"`
}

func (s *Server) threadStatePath(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "thread.json")
}

func (s *Server) runStatePath(runID string) string {
	return filepath.Join(s.dataRoot, "runs", runID+".json")
}

func (s *Server) threadHistoryPath(threadID string) string {
	return filepath.Join(s.threadRoot(threadID), "thread_history.json")
}

func (s *Server) persistSessionFile(session *Session) error {
	if session == nil {
		return nil
	}
	if err := os.MkdirAll(s.threadRoot(session.ThreadID), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(persistedThreadSession{
		ThreadID:  session.ThreadID,
		Messages:  session.Messages,
		Metadata:  session.Metadata,
		Status:    session.Status,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.threadStatePath(session.ThreadID), data, 0o644)
}

func (s *Server) persistRunFile(run *Run) error {
	return s.persistRunSnapshot(runSnapshotFromRun(run))
}

func (s *Server) persistRunSnapshot(snapshot harnessruntime.RunSnapshot) error {
	run := runFromSnapshot(snapshot)
	if run == nil {
		return nil
	}
	path := s.runStatePath(run.RunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(persistedThreadRun{
		RunID:       run.RunID,
		ThreadID:    run.ThreadID,
		AssistantID: run.AssistantID,
		Status:      run.Status,
		CreatedAt:   run.CreatedAt,
		UpdatedAt:   run.UpdatedAt,
		Events:      snapshot.Events,
		Error:       run.Error,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
