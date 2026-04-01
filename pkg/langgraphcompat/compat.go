package langgraphcompat

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/checkpoint"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/memory"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
	"github.com/axeprpr/deerflow-go/pkg/tools/builtin"
)

// LangGraph API-compatible server wrapper for deerflow-go
// Implements the endpoints expected by @langchain/langgraph-sdk

type Server struct {
	httpServer        *http.Server
	logger            *log.Logger
	llmProvider       llm.LLMProvider
	tools             *tools.Registry
	sandbox           *sandbox.Sandbox
	subagents         *subagent.Pool
	clarify           *clarification.Manager
	clarifyAPI        *clarification.API
	defaultModel      string
	maxTurns          int
	store             *checkpoint.PostgresStore
	startedAt         time.Time
	sessions          map[string]*Session
	sessionsMu        sync.RWMutex
	runs              map[string]*Run
	runsMu            sync.RWMutex
	runStreams        map[string]map[uint64]chan StreamEvent
	runStreamSeq      uint64
	dataRoot          string
	uiStateMu         sync.RWMutex
	skills            map[string]gatewaySkill
	mcpConfig         gatewayMCPConfig
	agents            map[string]gatewayAgent
	userProfile       string
	memory            gatewayMemoryResponse
	memoryStore       memory.Storage
	memoryStoreCloser interface{ Close() }
	memorySvc         *memory.Service
	memoryThread      string
	mcpMu             sync.Mutex
	mcpClients        map[string]gatewayMCPClient
	mcpToolNames      map[string]struct{}
	mcpDeferredTools  []models.Tool
	mcpConnector      gatewayMCPConnector
	channelMu         sync.Mutex
	channelService    *gatewayChannelService
}

type HealthStatus struct {
	Status     string            `json:"status"`
	Components map[string]string `json:"components"`
	Uptime     time.Duration     `json:"uptime"`
}

type Session struct {
	ThreadID     string
	Messages     []models.Message
	Todos        []Todo
	Metadata     map[string]any
	Configurable map[string]any
	Status       string
	PresentFiles *tools.PresentFileRegistry
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Todo struct {
	Content string `json:"content,omitempty"`
	Status  string `json:"status,omitempty"`
}

type ThreadState struct {
	CheckpointID string         `json:"checkpoint_id,omitempty"`
	Values       map[string]any `json:"values"`
	Next         []string       `json:"next"`
	Tasks        []any          `json:"tasks"`
	Metadata     map[string]any `json:"metadata"`
	Config       map[string]any `json:"config,omitempty"`
	CreatedAt    string         `json:"created_at,omitempty"`
}

type Run struct {
	RunID        string
	ThreadID     string
	AssistantID  string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Events       []StreamEvent
	Error        string
	cancel       context.CancelFunc
	abandonTimer *time.Timer
}

const generalPurposeSubagentPrompt = "You are a general-purpose subagent working on a delegated task. Complete it autonomously and return a clear, actionable result.\n\n" +
	"<guidelines>\n" +
	"- Focus on completing the delegated task efficiently\n" +
	"- Use available tools as needed to accomplish the goal\n" +
	"- Think step by step but act decisively\n" +
	"- If you hit issues, explain them clearly in your response\n" +
	"- Do not ask for clarification; work with the provided information\n" +
	"</guidelines>"

const bashSubagentPrompt = "You are a bash command execution specialist. Execute the requested commands carefully and report results clearly.\n\n" +
	"<guidelines>\n" +
	"- Execute commands one at a time when they depend on each other\n" +
	"- Use parallel execution only when commands are independent\n" +
	"- Report both success/failure and relevant output\n" +
	"- Use absolute paths for file operations\n" +
	"- Be cautious with destructive operations\n" +
	"</guidelines>"

// LangGraph API types
type RunCreateRequest struct {
	AssistantID string         `json:"assistant_id"`
	ThreadID    string         `json:"thread_id,omitempty"`
	Input       map[string]any `json:"input,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
}

// Message represents a LangGraph-compatible message
type Message struct {
	Type             string         `json:"type"`
	ID               string         `json:"id"`
	Role             string         `json:"role,omitempty"`
	Content          any            `json:"content,omitempty"`
	Name             string         `json:"name,omitempty"`
	Data             map[string]any `json:"data,omitempty"`
	AdditionalKwargs map[string]any `json:"additional_kwargs,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
	UsageMetadata    map[string]int `json:"usage_metadata,omitempty"`
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
	clarifyManager := clarification.NewManager(32)
	registry.Register(builtin.BashTool())
	for _, tool := range builtin.FileTools() {
		registry.Register(tool)
	}
	for _, tool := range builtin.WebTools() {
		registry.Register(tool)
	}
	registry.Register(builtin.ViewImageTool())
	registry.Register(clarification.AskClarificationTool(clarifyManager))
	if acpAgents := loadACPAgentConfigs(); len(acpAgents) > 0 {
		registry.Register(tools.InvokeACPAgentTool(acpAgents))
	}
	var sb *sandbox.Sandbox
	sb, err := sandbox.New("langgraph", filepath.Join(os.TempDir(), "deerflow-langgraph-sandbox"))
	if err != nil {
		logger.Printf("Warning: failed to create sandbox: %v", err)
	}

	subagentAppCfg := loadSubagentsAppConfig()
	subagentExecutor := agent.NewSubagentExecutor(provider, registry, sb)
	subagentPool := subagent.NewPool(subagentExecutor, subagent.PoolConfig{
		MaxConcurrent: 2,
		Timeout:       subagentAppCfg.timeoutFor(subagent.SubagentGeneralPurpose),
		Defaults: map[subagent.SubagentType]subagent.SubagentConfig{
			subagent.SubagentGeneralPurpose: {
				Type:            subagent.SubagentGeneralPurpose,
				MaxTurns:        6,
				Timeout:         subagentAppCfg.timeoutFor(subagent.SubagentGeneralPurpose),
				SystemPrompt:    generalPurposeSubagentPrompt,
				DisallowedTools: []string{"task", "ask_clarification", "present_file", "present_files"},
			},
			subagent.SubagentBash: {
				Type:            subagent.SubagentBash,
				MaxTurns:        4,
				Timeout:         subagentAppCfg.timeoutFor(subagent.SubagentBash),
				SystemPrompt:    bashSubagentPrompt,
				Tools:           []string{"bash", "ls", "read_file", "write_file", "str_replace"},
				DisallowedTools: []string{"task", "ask_clarification", "present_file", "present_files"},
			},
		},
	})
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

	dataRoot := strings.TrimSpace(os.Getenv("DEERFLOW_DATA_ROOT"))
	if dataRoot == "" {
		dataRoot = filepath.Join(os.TempDir(), "deerflow-go-data")
	}
	dataRootAbs, err := filepath.Abs(dataRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataRootAbs, 0o755); err != nil {
		return nil, err
	}

	var memoryStore memory.Storage
	var memoryStoreCloser interface{ Close() }
	var memorySvc *memory.Service
	if dbURL != "" {
		postgresMemoryStore, err := memory.NewPostgresStore(ctx, dbURL)
		if err != nil {
			logger.Printf("Warning: failed to create memory store: %v", err)
		} else {
			memoryStore = postgresMemoryStore
			memoryStoreCloser = postgresMemoryStore
			timeout := 30 * time.Second
			if raw := strings.TrimSpace(os.Getenv("MEMORY_UPDATE_TIMEOUT")); raw != "" {
				if parsed, parseErr := time.ParseDuration(raw); parseErr == nil && parsed > 0 {
					timeout = parsed
				}
			}
			memoryModel := strings.TrimSpace(os.Getenv("MEMORY_LLM_MODEL"))
			if memoryModel == "" {
				memoryModel = defaultModel
			}
			memorySvc = memory.NewService(memoryStore, memory.NewLLMClient(provider, memoryModel)).WithUpdateTimeout(timeout)
		}
	}
	if memoryStore == nil {
		fileMemoryStore, err := memory.NewFileStore(filepath.Join(dataRootAbs, "memory"))
		if err != nil {
			logger.Printf("Warning: failed to create file-backed memory store: %v", err)
		} else {
			if err := fileMemoryStore.AutoMigrate(ctx); err != nil {
				logger.Printf("Warning: failed to initialize file-backed memory store: %v", err)
			} else {
				memoryStore = fileMemoryStore
				timeout := 30 * time.Second
				if raw := strings.TrimSpace(os.Getenv("MEMORY_UPDATE_TIMEOUT")); raw != "" {
					if parsed, parseErr := time.ParseDuration(raw); parseErr == nil && parsed > 0 {
						timeout = parsed
					}
				}
				memoryModel := strings.TrimSpace(os.Getenv("MEMORY_LLM_MODEL"))
				if memoryModel == "" {
					memoryModel = defaultModel
				}
				memorySvc = memory.NewService(memoryStore, memory.NewLLMClient(provider, memoryModel)).WithUpdateTimeout(timeout)
			}
		}
	}

	s := &Server{
		logger:            logger,
		llmProvider:       provider,
		tools:             registry,
		sandbox:           sb,
		subagents:         subagentPool,
		clarify:           clarifyManager,
		clarifyAPI:        clarification.NewAPI(clarifyManager),
		defaultModel:      defaultModel,
		maxTurns:          8,
		store:             store,
		startedAt:         time.Now().UTC(),
		sessions:          make(map[string]*Session),
		runs:              make(map[string]*Run),
		runStreams:        make(map[string]map[uint64]chan StreamEvent),
		dataRoot:          dataRootAbs,
		skills:            defaultGatewaySkills(),
		mcpConfig:         defaultGatewayMCPConfig(),
		agents:            map[string]gatewayAgent{},
		memory:            defaultGatewayMemory(),
		memoryStore:       memoryStore,
		memoryStoreCloser: memoryStoreCloser,
		memorySvc:         memorySvc,
		mcpClients:        map[string]gatewayMCPClient{},
		mcpToolNames:      map[string]struct{}{},
		mcpConnector:      defaultGatewayMCPConnector,
	}
	s.channelService = newGatewayChannelService()
	s.channelService.start()
	registry.Register(s.setupAgentTool())
	registry.Register(s.todoTool())
	if err := s.loadGatewayState(); err != nil {
		logger.Printf("Warning: failed to load gateway state: %v", err)
	}
	if err := s.loadGatewayCompatFiles(); err != nil {
		logger.Printf("Warning: failed to load gateway compatibility files: %v", err)
	}
	s.applyGatewayMCPConfig(ctx, s.mcpConfig)
	if err := s.loadPersistedSessions(); err != nil {
		logger.Printf("Warning: failed to load persisted sessions: %v", err)
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
	sandboxRef := cfg.Sandbox
	if sandboxRef == nil {
		sandboxRef = s.sandbox
	}
	registry := cfg.Tools
	if registry == nil {
		registry = s.tools
	}
	return agent.New(agent.AgentConfig{
		LLMProvider:     s.llmProvider,
		Tools:           registry,
		DeferredTools:   s.currentDeferredMCPTools(),
		PresentFiles:    cfg.PresentFiles,
		MaxTurns:        s.maxTurns,
		AgentType:       cfg.AgentType,
		Model:           cfg.Model,
		ReasoningEffort: cfg.ReasoningEffort,
		SystemPrompt:    cfg.SystemPrompt,
		Temperature:     cfg.Temperature,
		MaxTokens:       cfg.MaxTokens,
		Sandbox:         sandboxRef,
		RequestTimeout:  cfg.RequestTimeout,
	})
}

func (s *Server) currentDeferredMCPTools() []models.Tool {
	if s == nil {
		return nil
	}
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if len(s.mcpDeferredTools) == 0 {
		return nil
	}
	out := make([]models.Tool, 0, len(s.mcpDeferredTools))
	for _, tool := range s.mcpDeferredTools {
		out = append(out, tool)
	}
	return out
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	s.registerLangGraphRoutes(mux, "")
	s.registerLangGraphRoutes(mux, "/api/langgraph")
	s.registerGatewayRoutes(mux)
	s.registerDocsRoutes(mux)

	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)
}

func (s *Server) registerLangGraphRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc("POST "+prefix+"/runs/stream", s.handleRunsStream)
	mux.HandleFunc("POST "+prefix+"/runs", s.handleRunsCreate)
	mux.HandleFunc("GET "+prefix+"/runs/{run_id}", s.handleRunGet)
	mux.HandleFunc("GET "+prefix+"/runs/{run_id}/stream", s.handleRunStream)

	mux.HandleFunc("GET "+prefix+"/threads", s.handleThreadsList)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}", s.handleThreadGet)
	mux.HandleFunc("POST "+prefix+"/threads", s.handleThreadCreate)
	mux.HandleFunc("PATCH "+prefix+"/threads/{thread_id}", s.handleThreadUpdate)
	mux.HandleFunc("DELETE "+prefix+"/threads/{thread_id}", s.handleThreadDelete)
	mux.HandleFunc("POST "+prefix+"/threads/search", s.handleThreadSearch)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/files", s.handleThreadFiles)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/state", s.handleThreadStateGet)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/state", s.handleThreadStatePost)
	mux.HandleFunc("PATCH "+prefix+"/threads/{thread_id}/state", s.handleThreadStatePatch)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/history", s.handleThreadHistory)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/history", s.handleThreadHistory)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/runs", s.handleThreadRunsList)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/runs/{run_id}", s.handleThreadRunGet)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/runs", s.handleThreadRunsCreate)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/runs/stream", s.handleThreadRunsStream)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/runs/{run_id}/stream", s.handleThreadRunStream)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/stream", s.handleThreadJoinStream)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/clarifications", s.handleThreadClarificationCreate)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/clarifications/{id}", s.handleThreadClarificationGet)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/clarifications/{id}/resolve", s.handleThreadClarificationResolve)
}

func (s *Server) Start() error {
	s.logger.Printf("LangGraph-compatible server starting on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	var shutdownErr error
	if s.httpServer != nil {
		shutdownErr = s.httpServer.Shutdown(ctx)
	}
	if s.store != nil {
		s.store.Close()
	}
	if s.channelService != nil {
		s.channelService.stop()
	}
	if s.memoryStoreCloser != nil {
		s.memoryStoreCloser.Close()
	}
	s.closeGatewayMCPClients()
	if s.sandbox != nil {
		if err := s.sandbox.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}
	return shutdownErr
}

func (s *Server) healthStatus(ctx context.Context) HealthStatus {
	status := HealthStatus{
		Status:     "ok",
		Components: map[string]string{},
		Uptime:     time.Since(s.startedAt).Round(time.Second),
	}

	componentCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	status.Components["llm"] = s.checkLLMProvider(componentCtx)
	status.Components["database"] = s.checkDatabase(componentCtx)
	status.Components["sandbox"] = s.checkSandbox(componentCtx)

	overall := "ok"
	for name, componentStatus := range status.Components {
		switch componentStatus {
		case "down":
			if name == "llm" {
				overall = "down"
			} else if overall == "ok" {
				overall = "degraded"
			}
		case "disabled":
			if overall == "ok" {
				overall = "degraded"
			}
		}
	}
	status.Status = overall
	return status
}

func (s *Server) checkLLMProvider(ctx context.Context) string {
	if s == nil || s.llmProvider == nil {
		return "down"
	}
	if _, ok := s.llmProvider.(*llm.UnavailableProvider); ok {
		return "down"
	}
	stream, err := s.llmProvider.Stream(ctx, llm.ChatRequest{})
	if err == nil {
		for chunk := range stream {
			if chunk.Err != nil && !errors.Is(chunk.Err, context.DeadlineExceeded) {
				if errors.Is(chunk.Err, context.Canceled) || chunk.Err.Error() == "model is required" || chunk.Err.Error() == "messages are required" {
					return "ok"
				}
				return "down"
			}
		}
		return "ok"
	}
	if err.Error() == "model is required" || err.Error() == "messages are required" {
		return "ok"
	}
	return "down"
}

func (s *Server) checkDatabase(ctx context.Context) string {
	if s == nil || s.store == nil {
		return "disabled"
	}
	if err := s.store.Ping(ctx); err != nil {
		s.logger.Printf("health check: database down: %v", err)
		return "down"
	}
	return "ok"
}

func (s *Server) checkSandbox(ctx context.Context) string {
	if s == nil || s.sandbox == nil {
		return "disabled"
	}
	result, err := s.sandbox.Exec(ctx, "printf sandbox-ok", 2*time.Second)
	if err != nil {
		s.logger.Printf("health check: sandbox down: %v", err)
		return "down"
	}
	if result == nil || result.ExitCode() != 0 {
		return "down"
	}
	if result.Stdout() != "sandbox-ok" {
		s.logger.Printf("health check: sandbox unexpected output: %q", result.Stdout())
		return "down"
	}
	return "ok"
}
