package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/axeprpr/deerflow-go/pkg/langgraphcompat"
)

func main() {
	addr := flag.String("addr", defaultAddr(), "Server address")
	dbURL := flag.String("db", firstNonEmpty(os.Getenv("POSTGRES_URL")), "Postgres database URL")
	model := flag.String("model", firstNonEmpty(os.Getenv("DEFAULT_LLM_MODEL"), "qwen/Qwen3.5-9B"), "Default LLM model")
	flag.Parse()

	logger := log.Default()
	logger.Printf("Starting LangGraph-compatible server...")
	logger.Printf("  Address: %s", *addr)
	logger.Printf("  Database: %s", *dbURL)
	logger.Printf("  Model: %s", *model)
	if level := strings.TrimSpace(os.Getenv("LOG_LEVEL")); level != "" {
		logger.Printf("  Log Level: %s", level)
	}

	server, err := langgraphcompat.NewServer(*addr, *dbURL, *model)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Graceful shutdown
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
