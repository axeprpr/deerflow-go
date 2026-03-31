package langgraphcompat

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	deerflowmcp "github.com/axeprpr/deerflow-go/pkg/mcp"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type gatewayMCPClient interface {
	Tools(ctx context.Context) ([]models.Tool, error)
	Close() error
}

type gatewayMCPConnector func(ctx context.Context, name string, cfg gatewayMCPServerConfig) (gatewayMCPClient, error)

var (
	gatewayMCPConnectStdio = func(ctx context.Context, name, command string, env []string, args ...string) (gatewayMCPClient, error) {
		return deerflowmcp.ConnectStdio(ctx, name, command, env, args...)
	}
	gatewayMCPConnectSSE = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		return deerflowmcp.ConnectSSE(ctx, name, baseURL, headers, headerFunc)
	}
	gatewayMCPConnectHTTP = func(ctx context.Context, name, baseURL string, headers map[string]string, headerFunc func(context.Context) map[string]string) (gatewayMCPClient, error) {
		return deerflowmcp.ConnectHTTP(ctx, name, baseURL, headers, headerFunc)
	}
)

func defaultGatewayMCPConnector(ctx context.Context, name string, cfg gatewayMCPServerConfig) (gatewayMCPClient, error) {
	transportType := strings.TrimSpace(cfg.Type)
	if transportType == "" {
		transportType = "stdio"
	}
	switch transportType {
	case "stdio":
		command := strings.TrimSpace(cfg.Command)
		if command == "" {
			return nil, fmt.Errorf("stdio MCP server %q requires command", name)
		}
		return gatewayMCPConnectStdio(ctx, name, command, gatewayMCPEnv(cfg.Env), cfg.Args...)
	case "sse":
		baseURL := strings.TrimSpace(cfg.URL)
		if baseURL == "" {
			return nil, fmt.Errorf("sse MCP server %q requires url", name)
		}
		headers, headerFunc, err := gatewayMCPHeaders(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("sse MCP server %q oauth: %w", name, err)
		}
		return gatewayMCPConnectSSE(ctx, name, baseURL, headers, headerFunc)
	case "http", "streamable_http":
		baseURL := strings.TrimSpace(cfg.URL)
		if baseURL == "" {
			return nil, fmt.Errorf("http MCP server %q requires url", name)
		}
		headers, headerFunc, err := gatewayMCPHeaders(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("http MCP server %q oauth: %w", name, err)
		}
		return gatewayMCPConnectHTTP(ctx, name, baseURL, headers, headerFunc)
	default:
		return nil, fmt.Errorf("unsupported MCP transport type %q", transportType)
	}
}

func gatewayMCPHeaders(ctx context.Context, cfg gatewayMCPServerConfig) (map[string]string, func(context.Context) map[string]string, error) {
	headers := cloneGatewayMCPHeaders(cfg.Headers)
	provider, err := newGatewayMCPOAuthProvider(cfg.OAuth)
	if err != nil || provider == nil {
		return headers, nil, err
	}

	authHeader, err := provider.HeaderValue(ctx)
	if err != nil {
		return nil, nil, err
	}
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Authorization"] = authHeader

	return headers, func(ctx context.Context) map[string]string {
		authHeader, err := provider.HeaderValue(ctx)
		if err != nil || strings.TrimSpace(authHeader) == "" {
			return nil
		}
		return map[string]string{"Authorization": authHeader}
	}, nil
}

func cloneGatewayMCPHeaders(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = os.ExpandEnv(value)
	}
	return dst
}

func gatewayMCPEnv(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+os.ExpandEnv(values[key]))
	}
	return env
}

func (s *Server) applyGatewayMCPConfig(ctx context.Context, cfg gatewayMCPConfig) {
	if s == nil || s.tools == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	connector := s.mcpConnector
	if connector == nil {
		connector = defaultGatewayMCPConnector
	}

	type loadedServer struct {
		name   string
		client gatewayMCPClient
		tools  []models.Tool
	}

	serverNames := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	loaded := make([]loadedServer, 0, len(serverNames))
	for _, name := range serverNames {
		serverCfg := cfg.MCPServers[name]
		if !serverCfg.Enabled {
			continue
		}

		client, err := connector(ctx, name, serverCfg)
		if err != nil {
			s.logMCPError("connect", name, err)
			continue
		}
		tools, err := client.Tools(ctx)
		if err != nil {
			_ = client.Close()
			s.logMCPError("load tools", name, err)
			continue
		}
		loaded = append(loaded, loadedServer{name: name, client: client, tools: tools})
	}

	newClients := make(map[string]gatewayMCPClient, len(loaded))
	newToolNames := make(map[string]struct{})
	newTools := make([]models.Tool, 0)
	for _, server := range loaded {
		newClients[server.name] = server.client
		for _, tool := range server.tools {
			newToolNames[tool.Name] = struct{}{}
			newTools = append(newTools, tool)
		}
	}

	deferTools := shouldDeferMCPTools(newTools)

	s.mcpMu.Lock()
	oldClients := s.mcpClients
	oldToolNames := s.mcpToolNames
	s.mcpClients = newClients
	s.mcpToolNames = newToolNames
	if deferTools {
		s.mcpDeferredTools = append([]models.Tool(nil), newTools...)
	} else {
		s.mcpDeferredTools = nil
	}
	s.mcpMu.Unlock()

	for name := range oldToolNames {
		s.tools.Unregister(name)
	}
	if !deferTools {
		for _, tool := range newTools {
			if err := s.tools.Register(tool); err != nil {
				s.logMCPError("register tool "+tool.Name, tool.Name, err)
			}
		}
	}
	for name, client := range oldClients {
		if err := client.Close(); err != nil {
			s.logMCPError("close", name, err)
		}
	}
}

func (s *Server) closeGatewayMCPClients() {
	if s == nil {
		return
	}
	s.mcpMu.Lock()
	clients := s.mcpClients
	toolNames := s.mcpToolNames
	s.mcpClients = nil
	s.mcpToolNames = nil
	s.mcpDeferredTools = nil
	s.mcpMu.Unlock()

	for name := range toolNames {
		s.tools.Unregister(name)
	}
	for name, client := range clients {
		if err := client.Close(); err != nil {
			s.logMCPError("close", name, err)
		}
	}
}

func (s *Server) logMCPError(action, name string, err error) {
	if err == nil {
		return
	}
	logger := s.logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("MCP %s failed for %s: %v", strings.TrimSpace(action), strings.TrimSpace(name), err)
}

func shouldDeferMCPTools(tools []models.Tool) bool {
	raw := strings.TrimSpace(os.Getenv("DEERFLOW_TOOL_SEARCH_ENABLED"))
	if raw != "" {
		switch strings.ToLower(raw) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return len(tools) >= 8
}
