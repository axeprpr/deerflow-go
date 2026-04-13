package harnessruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const sqliteRunSnapshotSchemaSQL = `
pragma foreign_keys = on;

create table if not exists run_snapshots (
	run_id text primary key,
	thread_id text not null,
	snapshot_json blob not null
);

create index if not exists idx_run_snapshots_thread_id
	on run_snapshots(thread_id, run_id);
`

const sqliteRunEventSchemaSQL = `
pragma foreign_keys = on;

create table if not exists run_events (
	run_id text not null,
	event_index integer not null,
	event_json blob not null,
	primary key (run_id, event_index)
);

create index if not exists idx_run_events_run_id
	on run_events(run_id, event_index);
`

const sqliteThreadStateSchemaSQL = `
pragma foreign_keys = on;

create table if not exists thread_states (
	thread_id text primary key,
	state_json blob not null
);
`

type SQLiteRunSnapshotStore struct {
	db    *sql.DB
	codec RunSnapshotMarshaler
}

func NewSQLiteRunSnapshotStore(path string) (*SQLiteRunSnapshotStore, error) {
	db, err := openSQLiteStateDB(path, sqliteRunSnapshotSchemaSQL)
	if err != nil {
		return nil, err
	}
	return &SQLiteRunSnapshotStore{
		db:    db,
		codec: defaultRunSnapshotCodec(nil),
	}, nil
}

func (s *SQLiteRunSnapshotStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteRunSnapshotStore) LoadRunSnapshot(runID string) (RunSnapshot, bool) {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return RunSnapshot{}, false
	}
	var data []byte
	err := s.db.QueryRow(`select snapshot_json from run_snapshots where run_id = ?`, runID).Scan(&data)
	if err != nil {
		return RunSnapshot{}, false
	}
	snapshot, err := defaultRunSnapshotCodec(s.codec).Decode(data)
	if err != nil {
		return RunSnapshot{}, false
	}
	return cloneRunSnapshot(snapshot), true
}

func (s *SQLiteRunSnapshotStore) ListRunSnapshots(threadID string) []RunSnapshot {
	if s == nil || s.db == nil {
		return nil
	}
	var (
		rows *sql.Rows
		err  error
	)
	if strings.TrimSpace(threadID) != "" {
		rows, err = s.db.Query(`select snapshot_json from run_snapshots where thread_id = ? order by run_id`, threadID)
	} else {
		rows, err = s.db.Query(`select snapshot_json from run_snapshots order by run_id`)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	codec := defaultRunSnapshotCodec(s.codec)
	var out []RunSnapshot
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			continue
		}
		snapshot, err := codec.Decode(data)
		if err != nil {
			continue
		}
		out = append(out, cloneRunSnapshot(snapshot))
	}
	return out
}

func (s *SQLiteRunSnapshotStore) SaveRunSnapshot(snapshot RunSnapshot) {
	if s == nil || s.db == nil || strings.TrimSpace(snapshot.Record.RunID) == "" {
		return
	}
	data, err := defaultRunSnapshotCodec(s.codec).Encode(snapshot)
	if err != nil {
		return
	}
	_, _ = s.db.Exec(`
		insert into run_snapshots (run_id, thread_id, snapshot_json)
		values (?, ?, ?)
		on conflict (run_id) do update set
			thread_id = excluded.thread_id,
			snapshot_json = excluded.snapshot_json
	`, snapshot.Record.RunID, snapshot.Record.ThreadID, data)
}

type SQLiteRunEventStore struct {
	db               *sql.DB
	codec            RunEventLogMarshaler
	mu               sync.RWMutex
	streams          map[string]map[uint64]chan RunEvent
	nextSubscriberID map[string]uint64
}

func NewSQLiteRunEventStore(path string) (*SQLiteRunEventStore, error) {
	db, err := openSQLiteStateDB(path, sqliteRunEventSchemaSQL)
	if err != nil {
		return nil, err
	}
	return &SQLiteRunEventStore{
		db:               db,
		codec:            defaultRunEventLogCodec(nil),
		streams:          map[string]map[uint64]chan RunEvent{},
		nextSubscriberID: map[string]uint64{},
	}, nil
}

func (s *SQLiteRunEventStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteRunEventStore) NextRunEventIndex(runID string) int {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return 1
	}
	var max sql.NullInt64
	if err := s.db.QueryRow(`select max(event_index) from run_events where run_id = ?`, runID).Scan(&max); err != nil || !max.Valid {
		return 1
	}
	return int(max.Int64) + 1
}

func (s *SQLiteRunEventStore) AppendRunEvent(runID string, event RunEvent) {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return
	}
	data, err := defaultRunEventLogCodec(s.codec).Encode([]RunEvent{event})
	if err != nil {
		return
	}
	_, _ = s.db.Exec(`
		insert into run_events (run_id, event_index, event_json)
		values (
			?,
			coalesce((select max(event_index) + 1 from run_events where run_id = ?), 1),
			?
		)
	`, runID, runID, data)
	s.mu.RLock()
	subscribers := make([]chan RunEvent, 0, len(s.streams[runID]))
	for _, subscriber := range s.streams[runID] {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.RUnlock()
	for _, subscriber := range subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (s *SQLiteRunEventStore) LoadRunEvents(runID string) []RunEvent {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	rows, err := s.db.Query(`select event_json from run_events where run_id = ? order by event_index`, runID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	codec := defaultRunEventLogCodec(s.codec)
	var out []RunEvent
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			continue
		}
		events, err := codec.Decode(data)
		if err != nil || len(events) == 0 {
			continue
		}
		out = append(out, events[0])
	}
	return append([]RunEvent(nil), out...)
}

func (s *SQLiteRunEventStore) ReplaceRunEvents(runID string, events []RunEvent) {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`delete from run_events where run_id = ?`, runID); err != nil {
		return
	}
	codec := defaultRunEventLogCodec(s.codec)
	for i, event := range events {
		data, err := codec.Encode([]RunEvent{event})
		if err != nil {
			return
		}
		index := i + 1
		if _, err := tx.Exec(`insert into run_events (run_id, event_index, event_json) values (?, ?, ?)`, runID, index, data); err != nil {
			return
		}
	}
	_ = tx.Commit()
}

func (s *SQLiteRunEventStore) SubscribeRunEvents(runID string, buffer int) (<-chan RunEvent, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	s.mu.Lock()
	if s.streams[runID] == nil {
		s.streams[runID] = map[uint64]chan RunEvent{}
	}
	s.nextSubscriberID[runID]++
	id := s.nextSubscriberID[runID]
	live := make(chan RunEvent, buffer)
	s.streams[runID][id] = live
	s.mu.Unlock()

	polled, unsubscribePoll := newPollingRunEventSubscription(buffer, func() []RunEvent {
		return s.LoadRunEvents(runID)
	})
	out := make(chan RunEvent, buffer)
	done := make(chan struct{})
	go func() {
		defer close(out)
		seen := map[string]struct{}{}
		for {
			select {
			case <-done:
				return
			case event, ok := <-live:
				if !ok {
					live = nil
					continue
				}
				key := runEventSubscriptionKey(-1, event)
				if _, exists := seen[key]; exists {
					continue
				}
				select {
				case out <- event:
					seen[key] = struct{}{}
				case <-done:
					return
				}
			case event, ok := <-polled:
				if !ok {
					polled = nil
					continue
				}
				key := runEventSubscriptionKey(-1, event)
				if _, exists := seen[key]; exists {
					continue
				}
				select {
				case out <- event:
					seen[key] = struct{}{}
				case <-done:
					return
				}
			}
			if live == nil && polled == nil {
				return
			}
		}
	}()
	return out, func() {
		close(done)
		unsubscribePoll()
		s.mu.Lock()
		defer s.mu.Unlock()
		if streams := s.streams[runID]; streams != nil {
			if subscriber, ok := streams[id]; ok {
				delete(streams, id)
				close(subscriber)
			}
			if len(streams) == 0 {
				delete(s.streams, runID)
				delete(s.nextSubscriberID, runID)
			}
		}
	}
}

type SQLiteThreadStateStore struct {
	db    *sql.DB
	codec ThreadRuntimeStateMarshaler
}

func NewSQLiteThreadStateStore(path string) (*SQLiteThreadStateStore, error) {
	db, err := openSQLiteStateDB(path, sqliteThreadStateSchemaSQL)
	if err != nil {
		return nil, err
	}
	return &SQLiteThreadStateStore{
		db:    db,
		codec: defaultThreadRuntimeStateCodec(nil),
	}, nil
}

func (s *SQLiteThreadStateStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteThreadStateStore) LoadThreadRuntimeState(threadID string) (ThreadRuntimeState, bool) {
	if s == nil || s.db == nil || strings.TrimSpace(threadID) == "" {
		return ThreadRuntimeState{}, false
	}
	var data []byte
	err := s.db.QueryRow(`select state_json from thread_states where thread_id = ?`, threadID).Scan(&data)
	if err != nil {
		return ThreadRuntimeState{}, false
	}
	state, err := defaultThreadRuntimeStateCodec(s.codec).Decode(data)
	if err != nil {
		return ThreadRuntimeState{}, false
	}
	return cloneThreadRuntimeState(state), true
}

func (s *SQLiteThreadStateStore) ListThreadRuntimeStates() []ThreadRuntimeState {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`select state_json from thread_states order by thread_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	codec := defaultThreadRuntimeStateCodec(s.codec)
	var out []ThreadRuntimeState
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			continue
		}
		state, err := codec.Decode(data)
		if err != nil {
			continue
		}
		out = append(out, cloneThreadRuntimeState(state))
	}
	return out
}

func (s *SQLiteThreadStateStore) SaveThreadRuntimeState(state ThreadRuntimeState) {
	if s == nil || s.db == nil || strings.TrimSpace(state.ThreadID) == "" {
		return
	}
	data, err := defaultThreadRuntimeStateCodec(s.codec).Encode(state)
	if err != nil {
		return
	}
	_, _ = s.db.Exec(`
		insert into thread_states (thread_id, state_json)
		values (?, ?)
		on conflict (thread_id) do update set
			state_json = excluded.state_json
	`, state.ThreadID, data)
}

func (s *SQLiteThreadStateStore) HasThread(threadID string) bool {
	_, ok := s.LoadThreadRuntimeState(threadID)
	return ok
}

func (s *SQLiteThreadStateStore) MarkThreadStatus(threadID string, status string) {
	state, _ := s.LoadThreadRuntimeState(threadID)
	state.ThreadID = strings.TrimSpace(threadID)
	state.Status = strings.TrimSpace(status)
	s.SaveThreadRuntimeState(state)
}

func (s *SQLiteThreadStateStore) SetThreadMetadata(threadID string, key string, value any) {
	state, _ := s.LoadThreadRuntimeState(threadID)
	state.ThreadID = strings.TrimSpace(threadID)
	if state.Metadata == nil {
		state.Metadata = map[string]any{}
	}
	state.Metadata[key] = value
	s.SaveThreadRuntimeState(state)
}

func (s *SQLiteThreadStateStore) ClearThreadMetadata(threadID string, key string) {
	state, ok := s.LoadThreadRuntimeState(threadID)
	if !ok {
		return
	}
	delete(state.Metadata, key)
	s.SaveThreadRuntimeState(state)
}

func openSQLiteStateDB(path string, schema string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite runtime state schema: %w", err)
	}
	return db, nil
}
