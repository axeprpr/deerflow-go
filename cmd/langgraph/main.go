package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/axeprpr/deerflow-go/pkg/langgraphcompat"
)

//go:embed frontend/*
var frontendFS embed.FS

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	yolo := flag.Bool("yolo", false, "YOLO mode: no auth, defaults for all settings")
	authToken := flag.String("auth-token", os.Getenv("DEERFLOW_AUTH_TOKEN"), "Bearer token for API auth (env: DEERFLOW_AUTH_TOKEN)")
	addr := flag.String("addr", defaultAddr(), "Server address")
	dbURL := flag.String("db", firstNonEmpty(os.Getenv("DATABASE_URL"), os.Getenv("POSTGRES_URL")), "Database URL (postgres or sqlite)")
	model := flag.String("model", firstNonEmpty(os.Getenv("DEFAULT_LLM_MODEL"), "qwen/Qwen3.5-9B"), "Default LLM model")
	flag.Parse()

	logger := log.Default()
	logger.SetPrefix("[deerflow] ")

	// YOLO mode: zero-config defaults
	if *yolo {
		os.Setenv("DEERFLOW_YOLO", "1")
		os.Setenv("ADDR", ":8080")
		os.Setenv("DEFAULT_LLM_MODEL", "qwen/Qwen3.5-9B")
		os.Setenv("DEERFLOW_DATA_ROOT", "./data")
		os.Setenv("LOG_LEVEL", "info")
		if *addr == "" || *addr == ":8080" {
			*addr = ":8080"
		}
		if *model == "" {
			*model = "qwen/Qwen3.5-9B"
		}
	}

	if *authToken != "" {
		os.Setenv("DEERFLOW_AUTH_TOKEN", *authToken)
	}

	logger.Printf("Starting deerflow-go server...")
	logger.Printf("  YOLO mode: %v", *yolo)
	logger.Printf("  Address:   %s", *addr)
	logger.Printf("  Database: %s", describeDB(*dbURL))
	logger.Printf("  Model:    %s", *model)
	logger.Printf("  Auth:     %s", describeAuth(*authToken, *yolo))
	logger.Printf("  Version: %s (%s, %s)", version, commit, buildTime)
	if level := strings.TrimSpace(os.Getenv("LOG_LEVEL")); level != "" {
		logger.Printf("  Log Level: %s", level)
	}

	embeddedFrontend, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		log.Fatalf("Failed to prepare embedded frontend: %v", err)
	}

	server, err := langgraphcompat.NewServer(*addr, *dbURL, *model, langgraphcompat.WithFrontendFS(embeddedFrontend))
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Println("Shutting down...")
		cancel()
		server.Shutdown(ctx)
	}()

	logger.Printf("Server ready on %s", *addr)
	logger.Printf("  API docs: http://%s/docs", *addr)
	if err := server.Start(); err != nil {
		logger.Fatalf("Server error: %v", err)
	}
}

func defaultAddr() string {
	if addr := strings.TrimSpace(os.Getenv("ADDR")); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		if strings.HasPrefix(port, ":") {
			return port
		}
		return ":" + port
	}
	return ":8080"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func describeDB(dbURL string) string {
	if dbURL == "" {
		return "(file storage: $DEERFLOW_DATA_ROOT or /tmp/deerflow-go-data)"
	}
	return dbURL
}

func describeAuth(token string, yolo bool) string {
	if yolo {
		return "disabled (YOLO mode)"
	}
	if token == "" {
		return "disabled (no token set)"
	}
	return "enabled (Bearer token required)"
}
