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
	yolo := flag.Bool("yolo", false, "YOLO mode: use defaults for all settings (no config required)")
	addr := flag.String("addr", defaultAddr(), "Server address")
	dbURL := flag.String("db", firstNonEmpty(os.Getenv("POSTGRES_URL")), "Postgres database URL")
	model := flag.String("model", firstNonEmpty(os.Getenv("DEFAULT_LLM_MODEL"), "qwen/Qwen3.5-9B"), "Default LLM model")
	flag.Parse()

	logger := log.Default()
	logger.SetPrefix("[deerflow] ")

	if *yolo {
		os.Setenv("ADDR", ":8080")
		os.Setenv("DEFAULT_LLM_MODEL", "qwen/Qwen3.5-9B")
		os.Setenv("DEERFLOW_DATA_ROOT", "./data")
		os.Setenv("LOG_LEVEL", "info")
		*addr = ":8080"
		*model = "qwen/Qwen3.5-9B"
	}

	logger.Printf("Starting deerflow-go LangGraph-compatible server...")
	logger.Printf("  YOLO mode: %v", *yolo)
	logger.Printf("  Address:   %s", *addr)
	logger.Printf("  Database:  %s", describeDB(*dbURL))
	logger.Printf("  Model:     %s", *model)

	if level := strings.TrimSpace(os.Getenv("LOG_LEVEL")); level != "" {
		logger.Printf("  Log Level: %s", level)
	}

	server, err := langgraphcompat.NewServer(*addr, *dbURL, *model)
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
