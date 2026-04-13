package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	yolo := flag.Bool("yolo", false, "YOLO mode: no auth, defaults for all settings")
	cfg := langgraphcmd.DefaultConfig(defaultAddr())
	binding := langgraphcmd.BindFlags(flag.CommandLine, cfg)
	flag.Parse()

	logger := log.Default()
	logger.SetPrefix("[deerflow] ")

	// YOLO mode: zero-config defaults
	if *yolo {
		os.Setenv("DEERFLOW_YOLO", "1")
		os.Setenv("ADDR", ":8080")
		os.Setenv("DEERFLOW_DATA_ROOT", "./data")
		os.Setenv("LOG_LEVEL", "info")
	}
	cfg = binding.Config()
	if *yolo {
		if cfg.Addr == "" || cfg.Addr == ":8080" {
			cfg.Addr = ":8080"
		}
		if cfg.Model == "" {
			cfg.Model = "qwen/Qwen3.5-9B"
		}
		if cfg.Provider == "" {
			cfg.Provider = "siliconflow"
		}
	}
	if cfg.AuthToken != "" {
		os.Setenv("DEERFLOW_AUTH_TOKEN", cfg.AuthToken)
	}
	if err := cfg.Validate(); err != nil {
		logger.Fatalf("Invalid runtime configuration: %v", err)
	}

	logger.Printf("Starting deerflow-go server...")
	logger.Printf("  YOLO mode: %v", *yolo)
	logger.Printf("  Address:   %s", cfg.Addr)
	logger.Printf("  Database: %s", describeDB(cfg.DatabaseURL))
	logger.Printf("  Provider: %s", cfg.Provider)
	logger.Printf("  Model:    %s", cfg.Model)
	logger.Printf("  Runtime:  role=%s transport=%s worker_addr=%s worker_endpoint=%s sandbox=%s state=%s", cfg.Runtime.Role, cfg.Runtime.TransportBackend, cfg.Runtime.Addr, firstNonEmpty(cfg.Runtime.Endpoint, "(local)"), cfg.Runtime.SandboxBackend, firstNonEmpty(string(cfg.Runtime.StateBackend), "(default)"))
	logger.Printf("  Auth:     %s", describeAuth(cfg.AuthToken, *yolo))
	logger.Printf("  Version: %s (%s, %s)", version, commit, buildTime)
	if level := strings.TrimSpace(os.Getenv("LOG_LEVEL")); level != "" {
		logger.Printf("  Log Level: %s", level)
	}

	launcher, err := cfg.BuildLauncher()
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	logger.Printf("Server ready on %s", cfg.Addr)
	logger.Printf("  API docs: http://%s/docs", cfg.Addr)
	if err := commandrun.Run(logger, launcher, 15*time.Second, http.ErrServerClosed); err != nil {
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
