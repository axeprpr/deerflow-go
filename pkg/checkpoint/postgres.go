package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaSQL = `
create table if not exists sessions (
	id text primary key,
	user_id text not null,
	state text not null,
	metadata jsonb not null default '{}'::jsonb,
	created_at timestamptz not null,
	updated_at timestamptz not null
);

create table if not exists messages (
	id text primary key,
	session_id text not null references sessions(id) on delete cascade,
	role text not null,
	content text not null default '',
	tool_calls jsonb not null default '[]'::jsonb,
	tool_result jsonb,
	metadata jsonb not null default '{}'::jsonb,
	created_at timestamptz not null
);

create index if not exists idx_messages_session_created_at
	on messages(session_id, created_at, id);
`

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	store := &PostgresStore{pool: pool}
	if err := store.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) Migrate(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not initialized")
	}
	_, err := s.pool.Exec(ctx, schemaSQL)
	return err
}

func (s *PostgresStore) CreateSession(ctx context.Context, session models.Session) error {
	if err := session.Validate(); err != nil {
		return err
	}
	metadata, err := json.Marshal(defaultMap(session.Metadata))
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		insert into sessions (id, user_id, state, metadata, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (id) do nothing
	`, session.ID, session.UserID, session.State, metadata, session.CreatedAt, session.UpdatedAt)
	return err
}

func (s *PostgresStore) GetSession(ctx context.Context, id string) (models.Session, error) {
	row := s.pool.QueryRow(ctx, `
		select id, user_id, state, metadata, created_at, updated_at
		from sessions
		where id = $1
	`, id)

	var (
		session  models.Session
		metadata []byte
	)
	if err := row.Scan(&session.ID, &session.UserID, &session.State, &metadata, &session.CreatedAt, &session.UpdatedAt); err != nil {
		return models.Session{}, err
	}
	_ = json.Unmarshal(metadata, &session.Metadata)
	session.Messages, _ = s.ListMessages(ctx, id)
	return session, nil
}

func (s *PostgresStore) UpdateSession(ctx context.Context, session models.Session) error {
	if err := session.Validate(); err != nil {
		return err
	}
	metadata, err := json.Marshal(defaultMap(session.Metadata))
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		update sessions
		set user_id = $2, state = $3, metadata = $4, updated_at = $5
		where id = $1
	`, session.ID, session.UserID, session.State, metadata, session.UpdatedAt)
	return err
}

func (s *PostgresStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `delete from sessions where id = $1`, id)
	return err
}

func (s *PostgresStore) CreateMessage(ctx context.Context, msg models.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}

	toolCalls, err := json.Marshal(defaultToolCalls(msg.ToolCalls))
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(defaultMap(msg.Metadata))
	if err != nil {
		return err
	}

	var toolResult any
	if msg.ToolResult != nil {
		raw, err := json.Marshal(msg.ToolResult)
		if err != nil {
			return err
		}
		toolResult = raw
	}

	_, err = s.pool.Exec(ctx, `
		insert into messages (id, session_id, role, content, tool_calls, tool_result, metadata, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (id) do update
		set role = excluded.role,
			content = excluded.content,
			tool_calls = excluded.tool_calls,
			tool_result = excluded.tool_result,
			metadata = excluded.metadata,
			created_at = excluded.created_at
	`, msg.ID, msg.SessionID, msg.Role, msg.Content, toolCalls, toolResult, metadata, msg.CreatedAt)
	return err
}

func (s *PostgresStore) ListMessages(ctx context.Context, sessionID string) ([]models.Message, error) {
	rows, err := s.pool.Query(ctx, `
		select id, session_id, role, content, tool_calls, tool_result, metadata, created_at
		from messages
		where session_id = $1
		order by created_at asc, id asc
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var (
			msg        models.Message
			toolCalls  []byte
			toolResult []byte
			metadata   []byte
		)
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &toolCalls, &toolResult, &metadata, &msg.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(toolCalls, &msg.ToolCalls)
		if len(toolResult) > 0 {
			msg.ToolResult = &models.ToolResult{}
			_ = json.Unmarshal(toolResult, msg.ToolResult)
		}
		_ = json.Unmarshal(metadata, &msg.Metadata)
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (s *PostgresStore) DeleteMessages(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `delete from messages where session_id = $1`, sessionID)
	return err
}

func (s *PostgresStore) SaveSession(ctx context.Context, session models.Session) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := upsertSessionTx(ctx, tx, session); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from messages where session_id = $1`, session.ID); err != nil {
		return err
	}
	for _, msg := range session.Messages {
		if err := insertMessageTx(ctx, tx, msg); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func upsertSessionTx(ctx context.Context, tx pgx.Tx, session models.Session) error {
	metadata, err := json.Marshal(defaultMap(session.Metadata))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		insert into sessions (id, user_id, state, metadata, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (id) do update
		set user_id = excluded.user_id,
			state = excluded.state,
			metadata = excluded.metadata,
			updated_at = excluded.updated_at
	`, session.ID, session.UserID, session.State, metadata, session.CreatedAt, session.UpdatedAt)
	return err
}

func insertMessageTx(ctx context.Context, tx pgx.Tx, msg models.Message) error {
	toolCalls, err := json.Marshal(defaultToolCalls(msg.ToolCalls))
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(defaultMap(msg.Metadata))
	if err != nil {
		return err
	}

	var toolResult any
	if msg.ToolResult != nil {
		raw, err := json.Marshal(msg.ToolResult)
		if err != nil {
			return err
		}
		toolResult = raw
	}

	_, err = tx.Exec(ctx, `
		insert into messages (id, session_id, role, content, tool_calls, tool_result, metadata, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
	`, msg.ID, msg.SessionID, msg.Role, msg.Content, toolCalls, toolResult, metadata, zeroTime(msg.CreatedAt))
	return err
}

func defaultMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func defaultToolCalls(v []models.ToolCall) []models.ToolCall {
	if v == nil {
		return []models.ToolCall{}
	}
	return v
}

func zeroTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
