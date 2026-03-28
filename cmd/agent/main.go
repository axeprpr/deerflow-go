package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/checkpoint"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	deerflowmcp "github.com/axeprpr/deerflow-go/pkg/mcp"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

type config struct {
	Addr                  string        `env:"AGENT_ADDR" envDefault:":8080"`
	Provider              string        `env:"DEFAULT_LLM_PROVIDER" envDefault:"openai"`
	Model                 string        `env:"DEFAULT_LLM_MODEL" envDefault:"gpt-4.1-mini"`
	MaxTurns              int           `env:"AGENT_MAX_TURNS" envDefault:"8"`
	SubagentMaxConcurrent int           `env:"SUBAGENT_MAX_CONCURRENT" envDefault:"2"`
	SubagentTimeout       time.Duration `env:"SUBAGENT_TIMEOUT" envDefault:"2m"`
	DatabaseURL           string        `env:"DATABASE_URL"`
	SandboxRoot           string        `env:"SANDBOX_ROOT" envDefault:"/tmp/deerflow-sandbox"`
	MCPServerName         string        `env:"MCP_SERVER_NAME"`
	MCPServerCommand      string        `env:"MCP_SERVER_COMMAND"`
	MCPServerArgs         string        `env:"MCP_SERVER_ARGS"`
}

type app struct {
	cfg          config
	llmProvider  llm.LLMProvider
	tools        *tools.Registry
	sandbox      *sandbox.Sandbox
	store        sessionStore
	subagentPool *agent.SubagentPool
}

type runRequest struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
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

type sessionStore interface {
	Load(ctx context.Context, sessionID string) ([]models.Message, error)
	Save(ctx context.Context, session models.Session) error
}

type memoryStore struct {
	mu       sync.RWMutex
	sessions map[string]models.Session
}

var httpMessageSeq uint64

func main() {
	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatal(err)
	}

	root := &cobra.Command{
		Use: "agent",
	}
	root.AddCommand(newRunCommand(cfg), newServeCommand(cfg))
	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

func newRunCommand(cfg config) *cobra.Command {
	var (
		sessionID string
		userID    string
		modelName string
	)

	cmd := &cobra.Command{
		Use: "run --message <text>",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, cleanup, err := newApp(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()

			message, _ := cmd.Flags().GetString("message")
			message = strings.TrimSpace(message)
			if message == "" {
				return fmt.Errorf("message is required")
			}
			if sessionID == "" {
				sessionID = fmt.Sprintf("session-%d", time.Now().UTC().UnixNano())
			}

			result, err := app.run(cmd.Context(), runRequest{
				SessionID: sessionID,
				UserID:    defaultUserID(userID),
				Message:   message,
				Model:     modelName,
			})
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), result.Output)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier")
	cmd.Flags().StringVar(&userID, "user-id", "local", "user identifier")
	cmd.Flags().StringVar(&modelName, "model", cfg.Model, "model name")
	cmd.Flags().String("message", "", "input message")
	return cmd
}

func newServeCommand(cfg config) *cobra.Command {
	return &cobra.Command{
		Use: "serve",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, cleanup, err := newApp(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()

			mux := http.NewServeMux()
			mux.HandleFunc("/health", app.handleHealth)
			mux.HandleFunc("/run", app.handleRun)

			log.Printf("agent server listening on %s", cfg.Addr)
			return http.ListenAndServe(cfg.Addr, mux)
		},
	}
}

func newApp(ctx context.Context, cfg config) (*app, func(), error) {
	provider := llm.NewProvider(cfg.Provider)
	if provider == nil {
		return nil, nil, fmt.Errorf("unsupported llm provider %q", cfg.Provider)
	}

	sb, err := sandbox.New("main", cfg.SandboxRoot)
	if err != nil {
		return nil, nil, err
	}

	registry := tools.NewRegistry()
	registerBuiltins(registry)

	var cleanupFns []func()
	if strings.TrimSpace(cfg.MCPServerCommand) != "" {
		client, err := deerflowmcp.ConnectStdio(ctx, cfg.MCPServerName, cfg.MCPServerCommand, nil, strings.Fields(cfg.MCPServerArgs)...)
		if err != nil {
			return nil, nil, err
		}
		cleanupFns = append(cleanupFns, func() { _ = client.Close() })
		mcpTools, err := client.Tools(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, tool := range mcpTools {
			mustRegister(registry, tool)
		}
	}

	subagentPool := agent.NewSubagentPool(provider, registry, sb, cfg.SubagentMaxConcurrent, cfg.SubagentTimeout)
	mustRegister(registry, builtin.TaskTool(subagentPool))

	store := sessionStore(newMemoryStore())
	if strings.TrimSpace(cfg.DatabaseURL) != "" {
		pgStore, err := checkpoint.NewPostgresStore(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, err
		}
		store = &postgresSessionStore{store: pgStore}
		cleanupFns = append(cleanupFns, pgStore.Close)
	}

	cleanup := func() {
		for _, fn := range cleanupFns {
			fn()
		}
		_ = sb.Close()
	}

	return &app{
		cfg:          cfg,
		llmProvider:  provider,
		tools:        registry,
		sandbox:      sb,
		store:        store,
		subagentPool: subagentPool,
	}, cleanup, nil
}

func (a *app) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *app) handleRun(w http.ResponseWriter, r *http.Request) {
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
	req.UserID = defaultUserID(req.UserID)
	req.Message = strings.TrimSpace(req.Message)
	req.Model = strings.TrimSpace(req.Model)

	if req.SessionID == "" || req.Message == "" {
		http.Error(w, "session_id and message are required", http.StatusBadRequest)
		return
	}

	if wantsSSE(r) {
		a.streamRun(w, r, req)
		return
	}

	result, err := a.run(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, r.Context().Err()) {
			status = http.StatusRequestTimeout
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *app) run(ctx context.Context, req runRequest) (runResponse, error) {
	history, err := a.store.Load(ctx, req.SessionID)
	if err != nil {
		return runResponse{}, err
	}

	modelName := req.Model
	if modelName == "" {
		modelName = a.cfg.Model
	}

	history = append(history, models.Message{
		ID:        newHTTPMessageID("human"),
		SessionID: req.SessionID,
		Role:      models.RoleHuman,
		Content:   req.Message,
		CreatedAt: time.Now().UTC(),
	})

	runAgent := agent.New(agent.AgentConfig{
		LLMProvider: a.llmProvider,
		Tools:       a.tools,
		Sandbox:     a.sandbox,
		MaxTurns:    a.cfg.MaxTurns,
		Model:       modelName,
	})

	result, err := runAgent.Run(ctx, req.SessionID, history)
	if err != nil {
		return runResponse{}, err
	}

	if err := a.store.Save(ctx, models.Session{
		ID:        req.SessionID,
		UserID:    req.UserID,
		State:     models.SessionStateActive,
		Messages:  result.Messages,
		CreatedAt: firstCreatedAt(result.Messages),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		return runResponse{}, err
	}

	return runResponse{
		SessionID:    req.SessionID,
		Model:        modelName,
		Output:       result.FinalOutput,
		Usage:        result.Usage,
		MessageCount: len(result.Messages),
	}, nil
}

func (a *app) streamRun(w http.ResponseWriter, r *http.Request, req runRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}

	history, err := a.store.Load(r.Context(), req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	modelName := req.Model
	if modelName == "" {
		modelName = a.cfg.Model
	}

	history = append(history, models.Message{
		ID:        newHTTPMessageID("human"),
		SessionID: req.SessionID,
		Role:      models.RoleHuman,
		Content:   req.Message,
		CreatedAt: time.Now().UTC(),
	})

	runAgent := agent.New(agent.AgentConfig{
		LLMProvider: a.llmProvider,
		Tools:       a.tools,
		Sandbox:     a.sandbox,
		MaxTurns:    a.cfg.MaxTurns,
		Model:       modelName,
	})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range runAgent.Events() {
			writeSSE(w, "event", evt)
			flusher.Flush()
		}
	}()

	result, err := runAgent.Run(r.Context(), req.SessionID, history)
	<-done
	if err != nil {
		writeSSE(w, "error", map[string]string{"error": err.Error()})
		flusher.Flush()
		return
	}

	_ = a.store.Save(r.Context(), models.Session{
		ID:        req.SessionID,
		UserID:    req.UserID,
		State:     models.SessionStateActive,
		Messages:  result.Messages,
		CreatedAt: firstCreatedAt(result.Messages),
		UpdatedAt: time.Now().UTC(),
	})
	writeSSE(w, "done", runResponse{
		SessionID:    req.SessionID,
		Model:        modelName,
		Output:       result.FinalOutput,
		Usage:        result.Usage,
		MessageCount: len(result.Messages),
	})
	flusher.Flush()
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

func newMemoryStore() *memoryStore {
	return &memoryStore{sessions: make(map[string]models.Session)}
}

func (s *memoryStore) Load(_ context.Context, sessionID string) ([]models.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	return append([]models.Message(nil), session.Messages...), nil
}

func (s *memoryStore) Save(_ context.Context, session models.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

type postgresSessionStore struct {
	store *checkpoint.PostgresStore
}

func (s *postgresSessionStore) Load(ctx context.Context, sessionID string) ([]models.Message, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return session.Messages, nil
}

func (s *postgresSessionStore) Save(ctx context.Context, session models.Session) error {
	return s.store.SaveSession(ctx, session)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSSE(w http.ResponseWriter, event string, value any) {
	data, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream") || r.URL.Query().Get("stream") == "1"
}

func newHTTPMessageID(prefix string) string {
	seq := atomic.AddUint64(&httpMessageSeq, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UTC().UnixNano(), seq)
}

func defaultUserID(userID string) string {
	if strings.TrimSpace(userID) == "" {
		return "local"
	}
	return strings.TrimSpace(userID)
}

func firstCreatedAt(messages []models.Message) time.Time {
	if len(messages) == 0 {
		return time.Now().UTC()
	}
	return messages[0].CreatedAt
}
