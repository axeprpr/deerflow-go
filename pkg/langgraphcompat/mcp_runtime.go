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

func defaultGatewayMCPConnector(ctx context.Context, name string, cfg gatewayMCPServerConfig) (gatewayMCPClient, error) {
	transportType := strings.TrimSpace(cfg.Type)
	if transportType == "" {
		transportType = "stdio"
	}
	if transportType != "stdio" {
		return nil, fmt.Errorf("unsupported MCP transport type %q", transportType)
	}

	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		return nil, fmt.Errorf("stdio MCP server %q requires command", name)
	}

	return deerflowmcp.ConnectStdio(ctx, name, command, gatewayMCPEnv(cfg.Env), cfg.Args...)
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

	s.mcpMu.Lock()
	oldClients := s.mcpClients
	oldToolNames := s.mcpToolNames
	s.mcpClients = newClients
	s.mcpToolNames = newToolNames
	s.mcpMu.Unlock()

	for name := range oldToolNames {
		s.tools.Unregister(name)
	}
	for _, tool := range newTools {
		if err := s.tools.Register(tool); err != nil {
			s.logMCPError("register tool "+tool.Name, tool.Name, err)
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
