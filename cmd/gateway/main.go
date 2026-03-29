package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/gateway"
	"github.com/caarlos0/env/v11"
)

type config struct {
	Port              string `env:"GATEWAY_PORT" envDefault:":8080"`
	DatabaseURL       string `env:"DATABASE_URL" envDefault:""`
	OpenAIAPIKey      string `env:"OPENAI_API_KEY"`
	AnthropicAPIKey   string `env:"ANTHROPIC_API_KEY"`
	SiliconFlowAPIKey string `env:"SILICONFLOW_API_KEY"`
	DefaultModel      string `env:"DEFAULT_MODEL" envDefault:"openai/gpt-4o"`
}

func main() {
	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatal(err)
	}

	applyEnv(cfg)

	server, err := gateway.NewServer(gateway.Config{
		Addr:         normalizeAddr(cfg.Port),
		DatabaseURL:  cfg.DatabaseURL,
		DefaultModel: cfg.DefaultModel,
		Logger:       log.New(os.Stderr, "gateway ", log.LstdFlags),
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Fatal(err)
		}
	}
}

func applyEnv(cfg config) {
	if cfg.OpenAIAPIKey != "" {
		_ = os.Setenv("OPENAI_API_KEY", cfg.OpenAIAPIKey)
	}
	if cfg.AnthropicAPIKey != "" {
		_ = os.Setenv("ANTHROPIC_API_KEY", cfg.AnthropicAPIKey)
	}
	if cfg.SiliconFlowAPIKey != "" {
		_ = os.Setenv("SILICONFLOW_API_KEY", cfg.SiliconFlowAPIKey)
	}

	_, modelName := splitConfiguredModel(cfg.DefaultModel)
	if modelName != "" {
		_ = os.Setenv("DEFAULT_LLM_MODEL", modelName)
	}
}

func normalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return ":" + addr
}

func splitConfiguredModel(modelRef string) (string, string) {
	modelRef = strings.TrimSpace(modelRef)
	parts := strings.SplitN(modelRef, "/", 2)
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "openai", modelRef
}
