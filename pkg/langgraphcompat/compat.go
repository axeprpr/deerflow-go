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
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
)

// LangGraph API-compatible server wrapper for deerflow-go
// Implements the endpoints expected by @langchain/langgraph-sdk

type Server struct {
	httpServer *http.Server
	logger     *log.Logger
	agent      *agent.Agent
	store      *checkpoint.PostgresStore
	sessions   map[string]*Session
	sessionsMu sync.RWMutex
}

type Session struct {
	ThreadID  string
	Messages  []models.Message
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LangGraph API types
type RunCreateRequest struct {
	AssistantID string        `json:"assistant_id"`
	ThreadID    string        `json:"thread_id,omitempty"`
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
	event    string
	data     any
	runID    string
	threadID string
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

	// Create agent
	a := agent.New(agent.AgentConfig{
		LLMProvider: provider,
		Tools:       registry,
		MaxTurns:    8,
		Model:       defaultModel,
	})

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
		logger:   logger,
		agent:    a,
		store:    store,
		sessions: make(map[string]*Session),
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s, nil
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// LangGraph-compatible endpoints
	mux.HandleFunc("POST /runs/stream", s.handleRunsStream)
	mux.HandleFunc("GET /runs/{run_id}", s.handleRunGet)

	// Thread endpoints
	mux.HandleFunc("GET /threads/{thread_id}", s.handleThreadGet)
	mux.HandleFunc("POST /threads", s.handleThreadCreate)
	mux.HandleFunc("PUT /threads/{thread_id}", s.handleThreadUpdate)
	mux.HandleFunc("DELETE /threads/{thread_id}", s.handleThreadDelete)
	mux.HandleFunc("GET /threads/{thread_id}/history", s.handleThreadHistory)
	mux.HandleFunc("GET /threads", s.handleThreadList)

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
