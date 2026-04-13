package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
	"github.com/axeprpr/deerflow-go/pkg/langgraphcompat"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	yolo := flag.Bool("yolo", false, "YOLO mode: no auth, defaults for all settings")
	cfg := defaultConfig()
	authToken := flag.String("auth-token", cfg.AuthToken, "Bearer token for API auth (env: DEERFLOW_AUTH_TOKEN)")
	addr := flag.String("addr", cfg.Addr, "Server address")
	dbURL := flag.String("db", cfg.DatabaseURL, "Database URL (postgres or sqlite)")
	model := flag.String("model", cfg.Model, "Default LLM model")
	provider := flag.String("provider", cfg.Provider, "Default LLM provider")
	runtimeRole := flag.String("runtime-role", string(cfg.Runtime.Role), "Runtime node role: all-in-one|gateway")
	runtimeName := flag.String("runtime-name", cfg.Runtime.Name, "Runtime node name")
	runtimeRoot := flag.String("runtime-root", cfg.Runtime.Root, "Runtime node root")
	runtimeWorkerAddr := flag.String("runtime-worker-addr", cfg.Runtime.Addr, "Embedded runtime worker listen address")
	runtimeWorkerEndpoint := flag.String("runtime-worker-endpoint", cfg.Runtime.Endpoint, "Remote worker endpoint for gateway runtime role")
	maxTurns := flag.Int("max-turns", cfg.Runtime.MaxTurns, "Default agent max turns")
	flag.Parse()

	logger := log.Default()
	logger.SetPrefix("[deerflow] ")

	// YOLO mode: zero-config defaults
	if *yolo {
		os.Setenv("DEERFLOW_YOLO", "1")
		os.Setenv("ADDR", ":8080")
		os.Setenv("DEFAULT_LLM_PROVIDER", "siliconflow")
		os.Setenv("DEFAULT_LLM_MODEL", "qwen/Qwen3.5-9B")
		os.Setenv("DEERFLOW_DATA_ROOT", "./data")
		os.Setenv("LOG_LEVEL", "info")
		if *addr == "" || *addr == ":8080" {
			*addr = ":8080"
		}
		if *model == "" {
			*model = "qwen/Qwen3.5-9B"
		}
		if *provider == "" {
			*provider = "siliconflow"
		}
	}

	if *authToken != "" {
		os.Setenv("DEERFLOW_AUTH_TOKEN", *authToken)
	}
	if *provider != "" {
		os.Setenv("DEFAULT_LLM_PROVIDER", *provider)
	}
	runtimeConfig := runtimecmd.NodeConfig{
		Role:     runtimecmd.NormalizeRole(*runtimeRole, cfg.Runtime.Role),
		Addr:     runtimecmd.NormalizeAddr(*runtimeWorkerAddr, cfg.Runtime.Addr),
		Name:     strings.TrimSpace(*runtimeName),
		Root:     strings.TrimSpace(*runtimeRoot),
		DataRoot: cfg.Runtime.DataRoot,
		Provider: strings.TrimSpace(*provider),
		Endpoint: strings.TrimSpace(*runtimeWorkerEndpoint),
		MaxTurns: *maxTurns,
	}
	if err := runtimeConfig.ValidateForLangGraph(); err != nil {
		logger.Fatalf("Invalid runtime configuration: %v", err)
	}

	logger.Printf("Starting deerflow-go server...")
	logger.Printf("  YOLO mode: %v", *yolo)
	logger.Printf("  Address:   %s", *addr)
	logger.Printf("  Database: %s", describeDB(*dbURL))
	logger.Printf("  Provider: %s", *provider)
	logger.Printf("  Model:    %s", *model)
	logger.Printf("  Runtime:  role=%s worker_addr=%s worker_endpoint=%s", runtimeConfig.Role, runtimeConfig.Addr, firstNonEmpty(runtimeConfig.Endpoint, "(local)"))
	logger.Printf("  Auth:     %s", describeAuth(*authToken, *yolo))
	logger.Printf("  Version: %s (%s, %s)", version, commit, buildTime)
	if level := strings.TrimSpace(os.Getenv("LOG_LEVEL")); level != "" {
		logger.Printf("  Log Level: %s", level)
	}

	server, err := langgraphcompat.NewServer(*addr, *dbURL, *model,
		langgraphcompat.WithRuntimeNodeConfig(runtimeConfig.RuntimeNodeConfig()),
		langgraphcompat.WithMaxTurns(runtimeConfig.MaxTurns),
	)
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

type config struct {
	AuthToken   string
	Addr        string
	DatabaseURL string
	Provider    string
	Model       string
	Runtime     runtimecmd.NodeConfig
}

func defaultConfig() config {
	return config{
		AuthToken:   strings.TrimSpace(os.Getenv("DEERFLOW_AUTH_TOKEN")),
		Addr:        defaultAddr(),
		DatabaseURL: firstNonEmpty(os.Getenv("DATABASE_URL"), os.Getenv("POSTGRES_URL")),
		Provider:    firstNonEmpty(os.Getenv("DEFAULT_LLM_PROVIDER"), "siliconflow"),
		Model:       firstNonEmpty(os.Getenv("DEFAULT_LLM_MODEL"), "qwen/Qwen3.5-9B"),
		Runtime:     runtimecmd.DefaultLangGraphNodeConfig(),
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
