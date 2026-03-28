package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
)

type server struct {
	llmProvider  llm.LLMProvider
	tools        *tools.Registry
	sandbox      *sandbox.Sandbox
	defaultModel string
	maxTurns     int
	subagentPool *agent.SubagentPool
	sessionStore map[string][]models.Message
	mu           sync.RWMutex
}

type runRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Model     string `json:"model"`
}

type runResponse struct {
	SessionID    string       `json:"session_id"`
	Model        string       `json:"model"`
	Output       string       `json:"output"`
	Usage        *agent.Usage `json:"usage,omitempty"`
	MessageCount int          `json:"message_count"`
}

var httpMessageSeq uint64

func main() {
	cfg := loadConfig()
	provider := llm.NewProvider(cfg.Provider)
	if provider == nil {
		log.Fatalf("unsupported llm provider %q", cfg.Provider)
	}

	sb, _ := sandbox.New("main", "/tmp/deerflow-sandbox")
	registry := tools.NewRegistry()
	registerBuiltins(registry)

	subagentPool := agent.NewSubagentPool(provider, registry, sb, cfg.SubagentMaxConcurrent, cfg.SubagentTimeout)
	mustRegister(registry, builtin.TaskTool(subagentPool))

	srv := &server{
		llmProvider:  provider,
		tools:        registry,
		sandbox:      sb,
		defaultModel: cfg.Model,
		maxTurns:     cfg.MaxTurns,
		subagentPool: subagentPool,
		sessionStore: make(map[string][]models.Message),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/run", srv.handleRun)

	addr := cfg.Addr
	log.Printf("agent server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

type config struct {
	Addr                  string
	Provider              string
	Model                 string
	MaxTurns              int
	SubagentMaxConcurrent int
	SubagentTimeout       time.Duration
}

func loadConfig() config {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	addr := port
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}

	return config{
		Addr:                  addr,
		Provider:              envOr("DEFAULT_LLM_PROVIDER", "openai"),
		Model:                 envOr("DEFAULT_LLM_MODEL", "gpt-4.1-mini"),
		MaxTurns:              envInt("AGENT_MAX_TURNS", 8),
		SubagentMaxConcurrent: envInt("SUBAGENT_MAX_CONCURRENT", 2),
		SubagentTimeout:       envDuration("SUBAGENT_TIMEOUT", 2*time.Minute),
	}
}

func registerBuiltins(registry *tools.Registry) {
	mustRegister(registry, builtin.BashTool())
	for _, tool := range builtin.FileTools() {
		mustRegister(registry, tool)
	}
}

func mustRegister(registry *tools.Registry, tool models.Tool) {
	if err := registry.Register(tool); err != nil {
		log.Fatalf("register tool %s: %v", tool.Name, err)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Message = strings.TrimSpace(req.Message)
	req.Model = strings.TrimSpace(req.Model)
	if req.SessionID == "" || req.Message == "" {
		http.Error(w, "session_id and message are required", http.StatusBadRequest)
		return
	}

	modelName := s.defaultModel
	if req.Model != "" {
		modelName = req.Model
	}

	msg := models.Message{
		ID:        newHTTPMessageID("human"),
		SessionID: req.SessionID,
		Role:      models.RoleHuman,
		Content:   req.Message,
		CreatedAt: time.Now().UTC(),
	}

	history := s.loadSession(req.SessionID)
	history = append(history, msg)

	runAgent := agent.New(agent.AgentConfig{
		LLMProvider: s.llmProvider,
		Tools:       s.tools,
		Sandbox:     s.sandbox,
		MaxTurns:    s.maxTurns,
		Model:       modelName,
	})

	result, err := runAgent.Run(r.Context(), req.SessionID, history)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, r.Context().Err()) {
			status = http.StatusRequestTimeout
		}
		http.Error(w, err.Error(), status)
		return
	}

	s.storeSession(req.SessionID, result.Messages)
	writeJSON(w, http.StatusOK, runResponse{
		SessionID:    req.SessionID,
		Model:        modelName,
		Output:       result.FinalOutput,
		Usage:        result.Usage,
		MessageCount: len(result.Messages),
	})
}

func (s *server) loadSession(sessionID string) []models.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]models.Message(nil), s.sessionStore[sessionID]...)
}

func (s *server) storeSession(sessionID string, messages []models.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionStore[sessionID] = append([]models.Message(nil), messages...)
}

func newHTTPMessageID(prefix string) string {
	seq := atomic.AddUint64(&httpMessageSeq, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UTC().UnixNano(), seq)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}
