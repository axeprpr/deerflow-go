package langgraphcompat

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/agent"
	"github.com/axeprpr/deerflow-go/pkg/checkpoint"
	"github.com/axeprpr/deerflow-go/pkg/clarification"
	"github.com/axeprpr/deerflow-go/pkg/harness"
	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/sandbox"
	"github.com/axeprpr/deerflow-go/pkg/subagent"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

// LangGraph API-compatible server wrapper for deerflow-go
// Implements the endpoints expected by @langchain/langgraph-sdk

type Server struct {
	httpServer       *http.Server
	logger           *log.Logger
	llmProvider      llm.LLMProvider
	runtime          *harness.Runtime
	tools            *tools.Registry
	sandboxName      string
	sandboxRoot      string
	subagents        *subagent.Pool
	clarify          *clarification.Manager
	clarifyAPI       *clarification.API
	defaultModel     string
	maxTurns         int
	store            checkpoint.Store
	startedAt        time.Time
	sessions         map[string]*Session
	sessionsMu       sync.RWMutex
	runs             map[string]*Run
	runsMu           sync.RWMutex
	runRegistry      *runRegistry
	snapshotStore    harnessruntime.RunSnapshotStore
	eventStore       harnessruntime.RunEventStore
	threadStateStore harnessruntime.ThreadStateStore
	runDispatcher    harnessruntime.RunDispatcher
	sandboxManager   *harnessruntime.SandboxResourceManager
	runtimeSystem    *harnessruntime.RuntimeNode
	runtimeNode      harnessruntime.RuntimeNodeConfig
	dataRoot         string
	uiStateMu        sync.RWMutex
	models           map[string]gatewayModel
	skills           map[string]gatewaySkill
	mcpConfig        gatewayMCPConfig
	agents           map[string]gatewayAgent
	userProfile      string
	memoryCache      gatewayMemoryResponse
	memoryConfig     gatewayMemoryConfig
	channelMu        sync.Mutex
	channelService   *gatewayChannelService
	channelConfig    gatewayChannelsConfig
	compatFSManaged  bool
	mcpMu            sync.Mutex
	mcpConnector     gatewayMCPConnector
	mcpClients       map[string]gatewayMCPClient
	mcpToolNames     map[string]struct{}
	mcpDeferredTools []models.Tool
	channels         gatewayChannelsStatus
}

type HealthStatus struct {
	Status     string            `json:"status"`
	Components map[string]string `json:"components"`
	Uptime     time.Duration     `json:"uptime"`
}

type Session struct {
	CheckpointID string
	ThreadID     string
	Messages     []models.Message
	Todos        []Todo
	Values       map[string]any
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
	CheckpointID       string         `json:"checkpoint_id,omitempty"`
	ParentCheckpointID string         `json:"parent_checkpoint_id,omitempty"`
	Checkpoint         map[string]any `json:"checkpoint,omitempty"`
	ParentCheckpoint   map[string]any `json:"parent_checkpoint,omitempty"`
	Values             map[string]any `json:"values"`
	Config             map[string]any `json:"config,omitempty"`
	Next               []string       `json:"next"`
	Tasks              []any          `json:"tasks"`
	Interrupts         []any          `json:"interrupts,omitempty"`
	Metadata           map[string]any `json:"metadata"`
	CreatedAt          string         `json:"created_at,omitempty"`
}

type Run struct {
	RunID           string
	ThreadID        string
	AssistantID     string
	Attempt         int
	ResumeFromEvent int
	ResumeReason    string
	Status          string
	Outcome         harnessruntime.RunOutcomeDescriptor
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Events          []StreamEvent
	Error           string
}

// LangGraph API types
type RunCreateRequest struct {
	AssistantID      string         `json:"assistant_id"`
	AssistantIDX     string         `json:"assistantId,omitempty"`
	ThreadID         string         `json:"thread_id,omitempty"`
	ThreadIDX        string         `json:"threadId,omitempty"`
	Messages         []any          `json:"messages,omitempty"`
	Input            map[string]any `json:"input,omitempty"`
	Config           map[string]any `json:"config,omitempty"`
	Context          map[string]any `json:"context,omitempty"`
	StreamMode       any            `json:"stream_mode,omitempty"`
	StreamModeX      any            `json:"streamMode,omitempty"`
	StreamResumable  *bool          `json:"stream_resumable,omitempty"`
	StreamResumableX *bool          `json:"streamResumable,omitempty"`
	OnDisconnect     string         `json:"on_disconnect,omitempty"`
	OnDisconnectX    string         `json:"onDisconnect,omitempty"`
}

// Message represents a LangGraph-compatible message
type Message struct {
	Type             string         `json:"type"`
	ID               string         `json:"id"`
	Role             string         `json:"role,omitempty"`
	Content          any            `json:"content,omitempty"`
	Name             string         `json:"name,omitempty"`
	Status           string         `json:"status,omitempty"`
	Data             map[string]any `json:"data,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
	AdditionalKwargs map[string]any `json:"additional_kwargs,omitempty"`
	UsageMetadata    map[string]any `json:"usage_metadata,omitempty"`
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
	ID              string
	Event           string
	Data            any
	RunID           string
	ThreadID        string
	Attempt         int                                 `json:"-"`
	ResumeFromEvent int                                 `json:"-"`
	ResumeReason    string                              `json:"-"`
	Outcome         harnessruntime.RunOutcomeDescriptor `json:"-"`
}

type ServerOption func(*Server)

func NewServer(addr string, dbURL string, defaultModel string, options ...ServerOption) (*Server, error) {
	logger := log.Default()
	ctx := context.Background()

	// Create LLM provider
	provider := llm.NewProvider("siliconflow")

	clarifyManager := clarification.NewManager(32)
	sandboxRoot := filepath.Join(os.TempDir(), "deerflow-langgraph-sandbox")
	runtimeNode := harnessruntime.DefaultRuntimeNodeConfig("langgraph", sandboxRoot)
	dataRoot := tools.DataRootFromEnv()
	dataRootAbs, err := filepath.Abs(dataRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataRootAbs, 0o755); err != nil {
		return nil, err
	}
	s := &Server{
		logger:        logger,
		llmProvider:   provider,
		sandboxName:   "langgraph",
		sandboxRoot:   sandboxRoot,
		clarify:       clarifyManager,
		clarifyAPI:    clarification.NewAPI(clarifyManager),
		defaultModel:  defaultModel,
		maxTurns:      100,
		startedAt:     time.Now().UTC(),
		sessions:      make(map[string]*Session),
		runs:          make(map[string]*Run),
		runRegistry:   newRunRegistry(),
		runtimeNode:   runtimeNode,
		dataRoot:      dataRootAbs,
		models:        defaultGatewayModels(defaultModel),
		skills:        nil,
		mcpConfig:     defaultGatewayMCPConfig(),
		agents:        map[string]gatewayAgent{},
		memoryCache:   defaultGatewayMemory(),
		memoryConfig:  defaultGatewayMemoryConfig(dataRootAbs),
		channelConfig: gatewayChannelsConfig{},
		channels:      defaultGatewayChannelsStatus(),
	}

	bootstrap, err := harnessruntime.BuildDefaultRuntimeSystemWithMemory(ctx, runtimeNode, dataRootAbs, provider, clarifyManager, s.maxTurns, s.runtimeProfileBuilder)
	if err != nil {
		return nil, err
	}
	runtimeNodeInstance := bootstrap.Node
	s.tools = runtimeNodeInstance.ToolRegistry()
	s.subagents = runtimeNodeInstance.Subagents()
	if bootstrap.MemoryErr != nil {
		logger.Printf("Warning: failed to configure memory store: %v", bootstrap.MemoryErr)
	}

	// Create checkpoint store
	if dbURL != "" {
		var err error
		s.store, err = checkpoint.OpenStore(ctx, dbURL)
		if err != nil {
			logger.Printf("Warning: failed to create database store: %v", err)
		}
	}

	s.attachRuntimeSystem(runtimeNodeInstance)
	s.snapshotStore = newCompatRunStateStore(s, runtimeNodeInstance.SnapshotStore(), runtimeNodeInstance.EventStore())
	s.eventStore = s.snapshotStore.(harnessruntime.RunEventStore)
	s.threadStateStore = newCompatThreadStateStore(s, runtimeNodeInstance.ThreadStateStore())
	s.runtime = bootstrap.Runtime
	s.bindRuntimeNodeDispatch()
	s.skills = s.discoverGatewaySkills(nil)
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	if err := s.loadGatewayState(); err != nil {
		logger.Printf("Warning: failed to load gateway state: %v", err)
	}
	if err := s.bootstrapGatewayMemory(ctx); err != nil {
		logger.Printf("Warning: failed to bootstrap gateway memory: %v", err)
	}
	s.loadPersistedThreads()
	s.loadPersistedRuns()

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: wrapCORS(mux),
	}

	return s, nil
}

func (s *Server) attachRuntimeSystem(node *harnessruntime.RuntimeNode) {
	if s == nil || node == nil {
		return
	}
	if s.runtime != nil {
		node.BindRuntime(s.runtime)
	} else if node.RuntimeView() != nil {
		s.runtime = node.RuntimeView()
	}
	s.runtimeSystem = node
	s.runtimeNode = node.Config
	s.sandboxManager = node.SandboxManager()
	if dispatcher := node.RunDispatcher(); dispatcher != nil {
		s.runDispatcher = dispatcher
	}
}

func (s *Server) runtimeDispatcher() harnessruntime.RunDispatcher {
	if s == nil {
		return nil
	}
	if node := s.runtimeSystem; node != nil {
		if dispatcher := node.RunDispatcher(); dispatcher != nil {
			return dispatcher
		}
	}
	return s.runDispatcher
}

func (s *Server) runtimeSandboxManager() *harnessruntime.SandboxResourceManager {
	if s == nil {
		return nil
	}
	if node := s.runtimeSystem; node != nil {
		if manager := node.SandboxManager(); manager != nil {
			return manager
		}
	}
	return s.sandboxManager
}

func (s *Server) toolRegistry() *tools.Registry {
	if s == nil {
		return nil
	}
	if node := s.runtimeSystem; node != nil {
		if registry := node.ToolRegistry(); registry != nil {
			return registry
		}
	}
	return s.tools
}

func (s *Server) subagentPool() *subagent.Pool {
	if s == nil {
		return nil
	}
	if node := s.runtimeSystem; node != nil {
		if pool := node.Subagents(); pool != nil {
			return pool
		}
	}
	return s.subagents
}

func (s *Server) runtimeView() *harness.Runtime {
	if s == nil {
		return nil
	}
	s.runtime = harnessruntime.RefreshHarnessRuntime(s.ensureRuntimeSystem(), s.llmProvider, s.maxTurns, s.runtime, s.runtimeProfileBuilder)
	return s.runtime
}

func (s *Server) bindRuntimeNodeDispatch() {
	if s == nil {
		return
	}
	s.ensureBoundRuntimeSystem()
}

func (s *Server) ensureRuntimeSystem() *harnessruntime.RuntimeNode {
	if s == nil {
		return nil
	}
	if s.runtimeSystem != nil {
		return s.runtimeSystem
	}
	node, err := harnessruntime.EnsureRuntimeNode(s.runtimeSystem, s.runtimeNodeConfig())
	if err != nil {
		return nil
	}
	s.attachRuntimeSystem(node)
	return node
}

func (s *Server) ensureBoundRuntimeSystem() *harnessruntime.RuntimeNode {
	if s == nil {
		return nil
	}
	node, err := harnessruntime.EnsureBoundRuntimeNode(s.runtimeSystem, s.runtimeNodeConfig(), s.runtimeView, s.runtimeWorkerSpecAdapter())
	if err != nil {
		return nil
	}
	s.attachRuntimeSystem(node)
	return node
}

func (s *Server) runtimeNodeConfig() harnessruntime.RuntimeNodeConfig {
	if s == nil {
		return harnessruntime.DefaultRuntimeNodeConfig("langgraph", filepath.Join(os.TempDir(), "deerflow-langgraph-sandbox"))
	}
	s.runtimeNode = harnessruntime.ResolveRuntimeNodeConfig(s.runtimeNode, s.sandboxName, s.sandboxRoot)
	return s.runtimeNode
}

func (s *Server) defaultSandboxRuntime(existing harness.SandboxRuntime) harness.SandboxRuntime {
	if existing != nil {
		return existing
	}
	if node := s.ensureRuntimeSystem(); node != nil {
		return node.ConfiguredSandboxRuntime()
	}
	return nil
}

func (s *Server) defaultRunDispatcher() harnessruntime.RunDispatcher {
	if s == nil {
		return harnessruntime.NewInProcessRunDispatcher()
	}
	if dispatcher := s.runtimeDispatcher(); dispatcher != nil {
		return dispatcher
	}
	if node := s.ensureBoundRuntimeSystem(); node != nil {
		return s.runtimeDispatcher()
	}
	return harnessruntime.NewInProcessRunDispatcher()
}

func (s *Server) defaultSandboxProvider(existing harness.SandboxProvider) harness.SandboxProvider {
	if existing != nil {
		return existing
	}
	if node := s.ensureRuntimeSystem(); node != nil {
		return node.ConfiguredSandboxProvider()
	}
	return nil
}

func (s *Server) newAgent(spec harness.AgentSpec) *agent.Agent {
	if s == nil {
		return agent.New(spec.AgentConfig())
	}
	runAgent, err := s.runtimeView().NewAgent(harness.AgentRequest{
		Spec:     spec,
		Features: harness.FeatureSet{Sandbox: true},
	})
	if err != nil {
		return agent.New(spec.AgentConfig())
	}
	return runAgent
}

func (s *Server) getOrCreateSandbox() (*sandbox.Sandbox, error) {
	if s == nil {
		return nil, errors.New("server is nil")
	}
	return s.runtimeView().SandboxProvider().Acquire()
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	s.registerLangGraphRoutes(mux, "")
	s.registerLangGraphRoutes(mux, "/api")
	s.registerLangGraphRoutes(mux, "/api/langgraph")
	s.registerGatewayRoutes(mux)
	s.registerDocsRoutes(mux)

	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)
}

func (s *Server) registerLangGraphRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc("POST "+prefix+"/runs/stream", s.handleRunsStream)
	mux.HandleFunc("GET "+prefix+"/runs/{run_id}", s.handleRunGet)
	mux.HandleFunc("GET "+prefix+"/runs/{run_id}/stream", s.handleRunStream)

	mux.HandleFunc("GET "+prefix+"/threads", s.handleThreadSearch)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}", s.handleThreadGet)
	mux.HandleFunc("POST "+prefix+"/threads", s.handleThreadCreate)
	mux.HandleFunc("PATCH "+prefix+"/threads/{thread_id}", s.handleThreadUpdate)
	mux.HandleFunc("PUT "+prefix+"/threads/{thread_id}", s.handleThreadUpdate)
	if prefix != "/api" {
		mux.HandleFunc("DELETE "+prefix+"/threads/{thread_id}", s.handleThreadDelete)
	}
	mux.HandleFunc("POST "+prefix+"/threads/search", s.handleThreadSearch)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/files", s.handleThreadFiles)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/state", s.handleThreadStateGet)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/state", s.handleThreadStatePost)
	mux.HandleFunc("PATCH "+prefix+"/threads/{thread_id}/state", s.handleThreadStatePatch)
	mux.HandleFunc("PUT "+prefix+"/threads/{thread_id}/state", s.handleThreadStatePost)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/history", s.handleThreadHistory)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/history", s.handleThreadHistory)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/runs", s.handleThreadRunsCreate)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/runs", s.handleThreadRunsList)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/runs/{run_id}", s.handleThreadScopedRunGet)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/runs/{run_id}/join", s.handleThreadRunJoin)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/runs/{run_id}/cancel", s.handleThreadRunCancel)
	mux.HandleFunc("POST "+prefix+"/threads/{thread_id}/runs/stream", s.handleThreadRunsStream)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/runs/{run_id}/stream", s.handleThreadRunStream)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/stream", s.handleThreadJoinStream)
	mux.HandleFunc("GET "+prefix+"/threads/{thread_id}/clarifications", s.handleThreadClarificationsList)
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
	s.closeGatewayMCPClients()
	s.stopGatewayChannels()
	if node := s.ensureRuntimeSystem(); node != nil {
		if err := node.Close(ctx); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	} else {
		if closer, ok := s.runtimeDispatcher().(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil && shutdownErr == nil {
				shutdownErr = err
			}
		}
		if manager := s.runtimeSandboxManager(); manager != nil {
			if err := manager.Close(); err != nil && shutdownErr == nil {
				shutdownErr = err
			}
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
	if s == nil || s.runtimeView() == nil || s.runtimeView().SandboxProvider() == nil {
		return "disabled"
	}
	sb, err := s.runtimeView().SandboxProvider().Acquire()
	if err != nil || sb == nil {
		if err != nil {
			s.logger.Printf("health check: sandbox acquire failed: %v", err)
		}
		return "down"
	}
	result, err := sb.Exec(ctx, "printf sandbox-ok", 2*time.Second)
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
