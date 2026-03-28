package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/checkpoint"
	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type config struct {
	DatabaseURL string `env:"DATABASE_URL,required"`
}

func main() {
	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatal(err)
	}

	root := &cobra.Command{
		Use: "checkpoint",
	}
	root.AddCommand(newMigrateCommand(cfg), newTestCommand(cfg))

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

func newMigrateCommand(cfg config) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run checkpoint schema migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := checkpoint.NewPostgresStore(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer store.Close()

			if err := store.AutoMigrate(cmd.Context()); err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "checkpoint migrations applied")
			return nil
		},
	}
}

func newTestCommand(cfg config) *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Create and read back a test session with messages",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			store, err := checkpoint.NewPostgresStore(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer store.Close()

			now := time.Now().UTC()
			sessionID := uuid.NewString()
			session := checkpoint.Session{
				ID:        sessionID,
				UserID:    "checkpoint-cli",
				State:     checkpoint.SessionStateActive,
				Metadata:  map[string]string{"source": "cmd/checkpoint"},
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := store.CreateSession(ctx, session); err != nil {
				return err
			}

			messages := []checkpoint.Message{
				{
					ID:        uuid.NewString(),
					SessionID: sessionID,
					Role:      checkpoint.RoleHuman,
					Content:   "checkpoint test user message",
					Metadata:  map[string]string{"step": "1"},
					CreatedAt: now,
				},
				{
					ID:        uuid.NewString(),
					SessionID: sessionID,
					Role:      checkpoint.RoleAI,
					Content:   "checkpoint test assistant reply",
					Metadata:  map[string]string{"step": "2"},
					CreatedAt: now.Add(time.Second),
				},
			}
			for _, msg := range messages {
				if err := store.CreateMessage(ctx, msg); err != nil {
					return err
				}
			}

			loaded, err := store.GetSession(ctx, sessionID)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "session=%s messages=%d\n", loaded.ID, len(loaded.Messages))
			return store.DeleteSession(context.Background(), sessionID)
		},
	}
}

func init() {
	log.SetOutput(os.Stderr)
}
