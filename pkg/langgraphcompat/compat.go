package langgraphcompat

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/checkpoint"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
)

// LangGraph API-compatible server wrapper for deerflow-go
// Implements the endpoints expected by @langchain/langgraph-sdk

type Server struct {
	httpServer   *http.Server
	logger       *log.Logger
	llmProvider  llm.LLMProvider
	tools        *tools.Registry
	subagents    *subagent.Pool
	defaultModel string
	maxTurns     int
	store        *checkpoint.PostgresStore
	sessions     map[string]*Session
	sessionsMu   sync.RWMutex
	runs         map[string]*Run
	runsMu       sync.RWMutex
}

type Session struct {
	ThreadID  string
	Messages  []models.Message
	Metadata  map[string]any
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ThreadState struct {
	CheckpointID string         `json:"checkpoint_id,omitempty"`
	Values       map[string]any `json:"values"`
	Next         []string       `json:"next"`
	Tasks        []any          `json:"tasks"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    string         `json:"created_at,omitempty"`
}

type Run struct {
	RunID       string
	ThreadID    string
	AssistantID string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Events      []StreamEvent
	Error       string
}

// LangGraph API types
type RunCreateRequest struct {
	AssistantID string         `json:"assistant_id"`
	ThreadID    string         `json:"thread_id,omitempty"`
	Input       map[string]any `json:"input,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
}

// Message represents a LangGraph-compatible message
type Message struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Role       string         `json:"role,omitempty"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
}

// ToolCall represents a LangGraph-compatible tool call
type ToolCall struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Args     map[string]any `json:"args"`
	RootID   string         `json:"root_id,omitempty"`
	ParentID string         `json:"parent_id,omitempty"`
}

type StreamEvent struct {
	ID       string
	Event    string
	Data     any
	RunID    string
	ThreadID string
}

func NewServer(addr string, dbURL string, defaultModel string) (*Server, error) {
	logger := log.Default()
	ctx := context.Background()

	// Create LLM provider
	provider := llm.NewProvider("siliconflow")

	// Create tool registry with built-in tools
	registry := tools.NewRegistry()
	registry.Register(builtin.BashTool())
	for _, tool := range builtin.FileTools() {
		registry.Register(tool)
	}
	subagentPool := agent.NewSubagentPool(provider, registry, nil, 2, 2*time.Minute)
	registry.Register(tools.TaskTool(subagentPool))

	// Create checkpoint store
	var store *checkpoint.PostgresStore
	if dbURL != "" {
		var err error
		store, err = checkpoint.NewPostgresStore(ctx, dbURL)
		if err != nil {
			logger.Printf("Warning: failed to create Postgres store: %v", err)
		}
	}

	s := &Server{
		logger:       logger,
		llmProvider:  provider,
		tools:        registry,
		subagents:    subagentPool,
		defaultModel: defaultModel,
		maxTurns:     8,
		store:        store,
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s, nil
}

func (s *Server) newAgent(cfg agent.AgentConfig) *agent.Agent {
	return agent.New(agent.AgentConfig{
		LLMProvider:     s.llmProvider,
		Tools:           s.tools,
		MaxTurns:        s.maxTurns,
		Model:           cfg.Model,
		ReasoningEffort: cfg.ReasoningEffort,
		Temperature:     cfg.Temperature,
		MaxTokens:       cfg.MaxTokens,
		Sandbox:         cfg.Sandbox,
	})
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// LangGraph-compatible endpoints
	mux.HandleFunc("POST /runs/stream", s.handleRunsStream)
	mux.HandleFunc("GET /runs/{run_id}", s.handleRunGet)
	mux.HandleFunc("GET /runs/{run_id}/stream", s.handleRunStream)

	// Thread endpoints
	mux.HandleFunc("GET /threads/{thread_id}", s.handleThreadGet)
	mux.HandleFunc("POST /threads", s.handleThreadCreate)
	mux.HandleFunc("PATCH /threads/{thread_id}", s.handleThreadUpdate)
	mux.HandleFunc("DELETE /threads/{thread_id}", s.handleThreadDelete)
	mux.HandleFunc("POST /threads/search", s.handleThreadSearch)
	mux.HandleFunc("GET /threads/{thread_id}/state", s.handleThreadStateGet)
	mux.HandleFunc("POST /threads/{thread_id}/state", s.handleThreadStatePost)
	mux.HandleFunc("PATCH /threads/{thread_id}/state", s.handleThreadStatePatch)
	mux.HandleFunc("POST /threads/{thread_id}/history", s.handleThreadHistory)
	mux.HandleFunc("POST /threads/{thread_id}/runs/stream", s.handleThreadRunsStream)
	mux.HandleFunc("GET /threads/{thread_id}/runs/{run_id}/stream", s.handleThreadRunStream)
	mux.HandleFunc("GET /threads/{thread_id}/stream", s.handleThreadJoinStream)

	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)
}

func (s *Server) Start() error {
	s.logger.Printf("LangGraph-compatible server starting on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
