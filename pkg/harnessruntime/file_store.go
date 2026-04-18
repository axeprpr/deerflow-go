package harnessruntime

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type JSONFileRunStore struct {
	root  string
	mu    sync.Mutex
	codec RunSnapshotMarshaler
}

func NewJSONFileRunStore(root string) *JSONFileRunStore {
	return &JSONFileRunStore{
		root:  strings.TrimSpace(root),
		codec: defaultRunSnapshotCodec(nil),
	}
}

func (s *JSONFileRunStore) LoadRunSnapshot(runID string) (RunSnapshot, bool) {
	if s == nil || strings.TrimSpace(runID) == "" || s.root == "" {
		return RunSnapshot{}, false
	}
	data, err := os.ReadFile(filepath.Join(s.root, runID+".json"))
	if err != nil {
		return RunSnapshot{}, false
	}
	codec := defaultRunSnapshotCodec(s.codec)
	snapshot, err := codec.Decode(data)
	if err != nil {
		return RunSnapshot{}, false
	}
	return cloneRunSnapshot(snapshot), true
}

func (s *JSONFileRunStore) ListRunSnapshots(threadID string) []RunSnapshot {
	if s == nil || s.root == "" {
		return nil
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil
	}
	out := make([]RunSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		snapshot, ok := s.LoadRunSnapshot(strings.TrimSuffix(entry.Name(), ".json"))
		if !ok {
			continue
		}
		if threadID != "" && snapshot.Record.ThreadID != threadID {
			continue
		}
		out = append(out, snapshot)
	}
	return out
}

func (s *JSONFileRunStore) SaveRunSnapshot(snapshot RunSnapshot) {
	if s == nil || strings.TrimSpace(snapshot.Record.RunID) == "" || s.root == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.MkdirAll(s.root, 0o755)
	codec := defaultRunSnapshotCodec(s.codec)
	data, err := codec.Encode(snapshot)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.root, snapshot.Record.RunID+".json"), data, 0o644)
}

func (s *JSONFileRunStore) TryCancelStaleRun(runID string, staleBefore time.Time) (RunRecord, bool) {
	if s == nil || strings.TrimSpace(runID) == "" || s.root == "" {
		return RunRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.root, strings.TrimSpace(runID)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return RunRecord{}, false
	}
	codec := defaultRunSnapshotCodec(s.codec)
	snapshot, err := codec.Decode(data)
	if err != nil || !canCancelDetachedRecord(snapshot.Record, staleBefore) {
		return RunRecord{}, false
	}
	snapshot.Record = applyDetachedCancel(snapshot.Record, time.Now().UTC())
	encoded, err := codec.Encode(snapshot)
	if err != nil {
		return RunRecord{}, false
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return RunRecord{}, false
	}
	return snapshot.Record, true
}

func (s *JSONFileRunStore) NextRunEventIndex(runID string) int {
	snapshot, ok := s.LoadRunSnapshot(runID)
	if !ok {
		return 1
	}
	return len(snapshot.Events) + 1
}

func (s *JSONFileRunStore) AppendRunEvent(runID string, event RunEvent) {
	snapshot, ok := s.LoadRunSnapshot(runID)
	if !ok {
		snapshot = RunSnapshot{Record: RunRecord{RunID: runID}}
	}
	snapshot.Events = append(snapshot.Events, event)
	s.SaveRunSnapshot(snapshot)
}

func (s *JSONFileRunStore) LoadRunEvents(runID string) []RunEvent {
	snapshot, ok := s.LoadRunSnapshot(runID)
	if !ok {
		return nil
	}
	return append([]RunEvent(nil), snapshot.Events...)
}
