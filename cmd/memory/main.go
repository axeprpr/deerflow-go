package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/caarlos0/env/v11"
	"github.com/spf13/cobra"
)

type config struct {
	DatabaseURL string        `env:"DATABASE_URL,required"`
	Provider    string        `env:"DEFAULT_LLM_PROVIDER" envDefault:"openai"`
	Model       string        `env:"DEFAULT_LLM_MODEL" envDefault:"gpt-4.1-mini"`
	Timeout     time.Duration `env:"MEMORY_TIMEOUT" envDefault:"30s"`
}

func main() {
	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatal(err)
	}

	root := &cobra.Command{Use: "memory"}
	root.AddCommand(newMigrateCommand(cfg), newGetCommand(cfg), newUpdateCommand(cfg))

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

func newMigrateCommand(cfg config) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run memory schema migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := memory.OpenStore(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer store.Close()
			return store.AutoMigrate(cmd.Context())
		},
	}
}

func newGetCommand(cfg config) *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "get --session-id <id>",
		Short: "Load the durable memory snapshot for a session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := memory.OpenStore(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer store.Close()

			doc, err := store.Load(cmd.Context(), sessionID)
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier")
	_ = cmd.MarkFlagRequired("session-id")
	return cmd
}

func newUpdateCommand(cfg config) *cobra.Command {
	var (
		sessionID string
		message   string
	)

	cmd := &cobra.Command{
		Use:   "update --session-id <id> --message <text>",
		Short: "Run a single memory extraction/update round",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := memory.OpenStore(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer store.Close()

			provider := llm.NewProvider(cfg.Provider)
			if provider == nil {
				return fmt.Errorf("unsupported llm provider %q", cfg.Provider)
			}

			service := memory.NewService(store, memory.NewLLMClient(provider, cfg.Model)).WithUpdateTimeout(cfg.Timeout)
			return service.Update(cmd.Context(), sessionID, []models.Message{
				{
					ID:        "memory-cli-user",
					SessionID: sessionID,
					Role:      models.RoleHuman,
					Content:   strings.TrimSpace(message),
					CreatedAt: time.Now().UTC(),
				},
			})
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier")
	cmd.Flags().StringVar(&message, "message", "", "conversation text to extract memory from")
	_ = cmd.MarkFlagRequired("session-id")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

func init() {
	log.SetOutput(os.Stderr)
	_ = context.Background()
}
