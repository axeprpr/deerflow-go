package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteSchemaSQL = `
pragma foreign_keys = on;

create table if not exists sessions (
	id text primary key,
	user_id text not null,
	state text not null default 'active',
	metadata text not null default '{}',
	created_at text not null,
	updated_at text not null
);

create table if not exists messages (
	id text primary key,
	session_id text not null references sessions(id) on delete cascade,
	role text not null,
	content text not null,
	tool_calls text,
	tool_result text,
	metadata text not null default '{}',
	created_at text not null
);

create index if not exists idx_messages_session
	on messages(session_id, created_at);
`

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
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
	store := &SQLiteStore{db: db}
	if err := store.Ping(ctx); err != nil {
		store.Close()
		return nil, err
	}
	if err := store.AutoMigrate(ctx); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() {
	if s != nil && s.db != nil {
		_ = s.db.Close()
	}
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}
	return nil
}

func (s *SQLiteStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not initialized")
	}
	if _, err := s.db.ExecContext(ctx, sqliteSchemaSQL); err != nil {
		return fmt.Errorf("migrate sqlite checkpoint schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Migrate(ctx context.Context) error { return s.AutoMigrate(ctx) }

func (s *SQLiteStore) CreateSession(ctx context.Context, session Session) error {
	if err := prepareSession(&session); err != nil {
		return err
	}
	metadata, err := json.Marshal(defaultMap(session.Metadata))
	if err != nil {
		return fmt.Errorf("marshal session metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		insert into sessions (id, user_id, state, metadata, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?)
	`, session.ID, session.UserID, session.State, string(metadata), formatDBTime(session.CreatedAt), formatDBTime(session.UpdatedAt))
	if err != nil {
		return fmt.Errorf("create session %q: %w", session.ID, err)
	}
	return nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, user_id, state, metadata, created_at, updated_at
		from sessions
		where id = ?
	`, id)
	session, err := scanSQLiteSession(row)
	if err != nil {
		return Session{}, fmt.Errorf("get session %q: %w", id, err)
	}
	messages, err := s.ListMessages(ctx, id)
	if err != nil {
		return Session{}, fmt.Errorf("get session %q messages: %w", id, err)
	}
	session.Messages = messages
	return session, nil
}

func (s *SQLiteStore) UpdateSession(ctx context.Context, session Session) error {
	if err := prepareSession(&session); err != nil {
		return err
	}
	metadata, err := json.Marshal(defaultMap(session.Metadata))
	if err != nil {
		return fmt.Errorf("marshal session metadata: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
		update sessions
		set user_id = ?, state = ?, metadata = ?, created_at = ?, updated_at = ?
		where id = ?
	`, session.UserID, session.State, string(metadata), formatDBTime(session.CreatedAt), formatDBTime(session.UpdatedAt), session.ID)
	if err != nil {
		return fmt.Errorf("update session %q: %w", session.ID, err)
	}
	if err := ensureRowsAffected(result, ErrNotFound); err != nil {
		return fmt.Errorf("update session %q: %w", session.ID, err)
	}
	return nil
}

func (s *SQLiteStore) UpdateSessionState(ctx context.Context, sessionID string, state SessionState) error {
	if err := state.Validate(); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		update sessions
		set state = ?, updated_at = ?
		where id = ?
	`, state, formatDBTime(time.Now().UTC()), sessionID)
	if err != nil {
		return fmt.Errorf("update session %q state: %w", sessionID, err)
	}
	if err := ensureRowsAffected(result, ErrNotFound); err != nil {
		return fmt.Errorf("update session %q state: %w", sessionID, err)
	}
	return nil
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `delete from sessions where id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	if err := ensureRowsAffected(result, ErrNotFound); err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	return nil
}

func (s *SQLiteStore) SaveSession(ctx context.Context, session Session) error {
	session = normalizeSessionMessages(session)
	if err := prepareSession(&session); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := s.upsertSession(ctx, tx, session); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from messages where session_id = ?`, session.ID); err != nil {
		return fmt.Errorf("replace messages for session %q: %w", session.ID, err)
	}
	for _, msg := range session.Messages {
		if err := prepareMessage(&msg); err != nil {
			return err
		}
		if err := s.insertMessage(ctx, tx, msg); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session %q: %w", session.ID, err)
	}
	committed = true
	return nil
}

func (s *SQLiteStore) CreateMessage(ctx context.Context, msg Message) error {
	if err := prepareMessage(&msg); err != nil {
		return err
	}
	if err := s.insertMessage(ctx, s.db, msg); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) SaveMessage(ctx context.Context, msg Message) error {
	if err := prepareMessage(&msg); err != nil {
		return err
	}
	toolCalls, err := encodeToolCalls(msg.ToolCalls)
	if err != nil {
		return err
	}
	toolResult, err := encodeToolResult(msg.ToolResult)
	if err != nil {
		return err
	}
	metadata, err := encodeMetadata(msg.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		insert into messages (id, session_id, role, content, tool_calls, tool_result, metadata, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(id) do update set
			session_id = excluded.session_id,
			role = excluded.role,
			content = excluded.content,
			tool_calls = excluded.tool_calls,
			tool_result = excluded.tool_result,
			metadata = excluded.metadata,
			created_at = excluded.created_at
	`, msg.ID, msg.SessionID, msg.Role, messageContent(msg), string(toolCalls), toolResult, string(metadata), formatDBTime(msg.CreatedAt))
	if err != nil {
		return fmt.Errorf("save message %q: %w", msg.ID, err)
	}
	return nil
}

func (s *SQLiteStore) GetMessage(ctx context.Context, id string) (Message, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, session_id, role, content, tool_calls, tool_result, metadata, created_at
		from messages
		where id = ?
	`, id)
	msg, err := scanSQLiteMessage(row)
	if err != nil {
		return Message{}, fmt.Errorf("get message %q: %w", id, err)
	}
	return msg, nil
}

func (s *SQLiteStore) UpdateMessage(ctx context.Context, msg Message) error {
	if err := prepareMessage(&msg); err != nil {
		return err
	}
	toolCalls, err := encodeToolCalls(msg.ToolCalls)
	if err != nil {
		return err
	}
	toolResult, err := encodeToolResult(msg.ToolResult)
	if err != nil {
		return err
	}
	metadata, err := encodeMetadata(msg.Metadata)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		update messages
		set session_id = ?, role = ?, content = ?, tool_calls = ?, tool_result = ?, metadata = ?, created_at = ?
		where id = ?
	`, msg.SessionID, msg.Role, messageContent(msg), string(toolCalls), toolResult, string(metadata), formatDBTime(msg.CreatedAt), msg.ID)
	if err != nil {
		return fmt.Errorf("update message %q: %w", msg.ID, err)
	}
	if err := ensureRowsAffected(result, ErrNotFound); err != nil {
		return fmt.Errorf("update message %q: %w", msg.ID, err)
	}
	return nil
}

func (s *SQLiteStore) ListMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, session_id, role, content, tool_calls, tool_result, metadata, created_at
		from messages
		where session_id = ?
		order by created_at asc, id asc
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list messages for session %q: %w", sessionID, err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		msg, err := scanSQLiteMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan messages for session %q: %w", sessionID, err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list messages for session %q: %w", sessionID, err)
	}
	if messages == nil {
		messages = []Message{}
	}
	return messages, nil
}

func (s *SQLiteStore) LoadSession(ctx context.Context, sessionID string) ([]Message, error) {
	return s.ListMessages(ctx, sessionID)
}

func (s *SQLiteStore) DeleteMessage(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `delete from messages where id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete message %q: %w", id, err)
	}
	if err := ensureRowsAffected(result, ErrNotFound); err != nil {
		return fmt.Errorf("delete message %q: %w", id, err)
	}
	return nil
}

func (s *SQLiteStore) DeleteMessages(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `delete from messages where session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("delete messages for session %q: %w", sessionID, err)
	}
	return nil
}

func (s *SQLiteStore) upsertSession(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, session Session) error {
	metadata, err := json.Marshal(defaultMap(session.Metadata))
	if err != nil {
		return fmt.Errorf("marshal session metadata: %w", err)
	}
	_, err = execer.ExecContext(ctx, `
		insert into sessions (id, user_id, state, metadata, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?)
		on conflict(id) do update set
			user_id = excluded.user_id,
			state = excluded.state,
			metadata = excluded.metadata,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, session.ID, session.UserID, session.State, string(metadata), formatDBTime(session.CreatedAt), formatDBTime(session.UpdatedAt))
	if err != nil {
		return fmt.Errorf("upsert session %q: %w", session.ID, err)
	}
	return nil
}

func (s *SQLiteStore) insertMessage(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, msg Message) error {
	toolCalls, err := encodeToolCalls(msg.ToolCalls)
	if err != nil {
		return err
	}
	toolResult, err := encodeToolResult(msg.ToolResult)
	if err != nil {
		return err
	}
	metadata, err := encodeMetadata(msg.Metadata)
	if err != nil {
		return err
	}
	_, err = execer.ExecContext(ctx, `
		insert into messages (id, session_id, role, content, tool_calls, tool_result, metadata, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)
	`, msg.ID, msg.SessionID, msg.Role, messageContent(msg), string(toolCalls), toolResult, string(metadata), formatDBTime(msg.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert message %q: %w", msg.ID, err)
	}
	return nil
}

type sqliteScanner interface{ Scan(dest ...any) error }

func scanSQLiteSession(row sqliteScanner) (Session, error) {
	var (
		session   Session
		metadata  string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&session.ID, &session.UserID, &session.State, &metadata, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	if strings.TrimSpace(metadata) != "" {
		if err := json.Unmarshal([]byte(metadata), &session.Metadata); err != nil {
			return Session{}, fmt.Errorf("unmarshal session metadata: %w", err)
		}
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	var err error
	session.CreatedAt, err = parseDBTime(createdAt)
	if err != nil {
		return Session{}, fmt.Errorf("parse session created_at: %w", err)
	}
	session.UpdatedAt, err = parseDBTime(updatedAt)
	if err != nil {
		return Session{}, fmt.Errorf("parse session updated_at: %w", err)
	}
	return session, nil
}

func scanSQLiteMessage(row sqliteScanner) (Message, error) {
	var (
		msg           Message
		toolCallsRaw  string
		toolResultRaw sql.NullString
		metadataRaw   string
		createdAt     string
	)
	if err := row.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &toolCallsRaw, &toolResultRaw, &metadataRaw, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, ErrNotFound
		}
		return Message{}, err
	}
	if strings.TrimSpace(toolCallsRaw) != "" {
		if err := json.Unmarshal([]byte(toolCallsRaw), &msg.ToolCalls); err != nil {
			return Message{}, fmt.Errorf("unmarshal message tool_calls: %w", err)
		}
	}
	if toolResultRaw.Valid && strings.TrimSpace(toolResultRaw.String) != "" {
		var result ToolResult
		if err := json.Unmarshal([]byte(toolResultRaw.String), &result); err != nil {
			return Message{}, fmt.Errorf("unmarshal message tool_result: %w", err)
		}
		msg.ToolResult = &result
	}
	if strings.TrimSpace(metadataRaw) != "" {
		if err := json.Unmarshal([]byte(metadataRaw), &msg.Metadata); err != nil {
			return Message{}, fmt.Errorf("unmarshal message metadata: %w", err)
		}
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]string{}
	}
	if msg.ToolCalls == nil {
		msg.ToolCalls = []ToolCall{}
	}
	var err error
	msg.CreatedAt, err = parseDBTime(createdAt)
	if err != nil {
		return Message{}, fmt.Errorf("parse message created_at: %w", err)
	}
	return msg, nil
}

func ensureRowsAffected(result sql.Result, notFound error) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return notFound
	}
	return nil
}

func formatDBTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
