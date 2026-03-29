package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/axeprpr/deerflow-go/pkg/langgraphcompat"
)

func main() {
	addr := flag.String("addr", ":8080", "Server address")
	dbURL := flag.String("db", "", "Postgres database URL")
	model := flag.String("model", "qwen/Qwen3.5-9B", "Default LLM model")
	flag.Parse()

	logger := log.Default()
	logger.Printf("Starting LangGraph-compatible server...")
	logger.Printf("  Address: %s", *addr)
	logger.Printf("  Database: %s", *dbURL)
	logger.Printf("  Model: %s", *model)

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
