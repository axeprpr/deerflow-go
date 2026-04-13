package langgraphcmd

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

type CommandOptions struct {
	Stderr io.Writer
	Args   []string
}

func RunCommand(fs *flag.FlagSet, build BuildInfo, options CommandOptions) error {
	if fs == nil {
		fs = flag.CommandLine
	}
	logger := log.New(orStderr(options.Stderr), "[deerflow] ", log.LstdFlags)

	yolo := fs.Bool("yolo", false, "YOLO mode: no auth, defaults for all settings")
	cfg := DefaultConfig()
	binding := BindFlags(fs, cfg)
	if err := fs.Parse(commandArgs(fs, options.Args)); err != nil {
		return err
	}
	cfg = binding.Config()

	if *yolo {
		os.Setenv("DEERFLOW_YOLO", "1")
		os.Setenv("ADDR", ":8080")
		os.Setenv("DEERFLOW_DATA_ROOT", "./data")
		os.Setenv("LOG_LEVEL", "info")
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
		return fmt.Errorf("invalid runtime configuration: %w", err)
	}

	logger.Printf("Starting deerflow-go server...")
	logger.Printf("  YOLO mode: %v", *yolo)
	logger.Printf("  Address:   %s", cfg.Addr)
	logger.Printf("  Database: %s", describeDB(cfg.DatabaseURL))
	logger.Printf("  Provider: %s", cfg.Provider)
	logger.Printf("  Model:    %s", cfg.Model)
	logger.Printf("  Runtime:  role=%s transport=%s worker_addr=%s worker_endpoint=%s sandbox=%s state=%s", cfg.Runtime.Role, cfg.Runtime.TransportBackend, cfg.Runtime.Addr, firstNonEmpty(cfg.Runtime.Endpoint, "(local)"), cfg.Runtime.SandboxBackend, firstNonEmpty(string(cfg.Runtime.StateBackend), "(default)"))
	logger.Printf("  Auth:     %s", describeAuth(cfg.AuthToken, *yolo))
	logger.Printf("  Version: %s (%s, %s)", build.Version, build.Commit, build.BuildTime)
	if level := strings.TrimSpace(os.Getenv("LOG_LEVEL")); level != "" {
		logger.Printf("  Log Level: %s", level)
	}

	launcher, err := cfg.BuildLauncher()
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	logger.Printf("Server ready on %s", cfg.Addr)
	logger.Printf("  API docs: http://%s/docs", cfg.Addr)
	return commandrun.Run(logger, launcher, 15*time.Second, http.ErrServerClosed)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func orStderr(w io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return os.Stderr
}

func commandArgs(fs *flag.FlagSet, explicit []string) []string {
	if explicit != nil {
		return explicit
	}
	if fs == flag.CommandLine {
		return os.Args[1:]
	}
	return nil
}
