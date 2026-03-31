package langgraphcompat

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var supportedGatewayChannels = []string{"feishu", "slack", "telegram"}

type channelStatusSnapshot struct {
	ServiceRunning bool                   `json:"service_running"`
	Channels       map[string]channelInfo `json:"channels"`
}

type channelInfo struct {
	Enabled bool `json:"enabled"`
	Running bool `json:"running"`
}

type gatewayChannelService struct {
	mu            sync.RWMutex
	configPresent bool
	config        gatewayChannelsConfig
	running       bool
	channels      map[string]channelInfo
}

type gatewayChannelsConfig struct {
	Channels map[string]map[string]any `yaml:"channels"`
}

func newGatewayChannelService() *gatewayChannelService {
	svc := &gatewayChannelService{
		channels: make(map[string]channelInfo, len(supportedGatewayChannels)),
	}
	for _, name := range supportedGatewayChannels {
		svc.channels[name] = channelInfo{}
	}

	config, ok := loadGatewayChannelConfig()
	if ok {
		svc.configPresent = true
		svc.config = config
		for _, name := range supportedGatewayChannels {
			info := svc.channels[name]
			info.Enabled = config.enabled(name)
			svc.channels[name] = info
		}
	}
	return svc
}

func (s *Server) ensureGatewayChannelService() *gatewayChannelService {
	if s == nil {
		return newGatewayChannelService()
	}
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	if s.channelService == nil {
		s.channelService = newGatewayChannelService()
		s.channelService.start()
	}
	return s.channelService
}

func (s *Server) gatewayChannelStatus() channelStatusSnapshot {
	return s.ensureGatewayChannelService().snapshot()
}

func (c gatewayChannelsConfig) enabled(name string) bool {
	if len(c.Channels) == 0 {
		return false
	}
	channel, ok := c.Channels[name]
	if !ok {
		return false
	}
	value, ok := channel["enabled"].(bool)
	return ok && value
}

func loadGatewayChannelConfig() (gatewayChannelsConfig, bool) {
	path, ok := resolveGatewayConfigPath()
	if !ok {
		return gatewayChannelsConfig{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return gatewayChannelsConfig{}, false
	}

	var raw struct {
		Channels map[string]any `yaml:"channels"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return gatewayChannelsConfig{}, false
	}

	cfg := gatewayChannelsConfig{Channels: make(map[string]map[string]any)}
	for name, value := range raw.Channels {
		channelName := strings.ToLower(strings.TrimSpace(name))
		if channelName == "" {
			continue
		}
		channelMap, ok := value.(map[string]any)
		if !ok {
			continue
		}
		cfg.Channels[channelName] = channelMap
	}
	return cfg, true
}

func resolveGatewayConfigPath() (string, bool) {
	if path := strings.TrimSpace(os.Getenv("DEER_FLOW_CONFIG_PATH")); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, true
		}
		return "", false
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for _, candidate := range []string{
		filepath.Join(wd, "config.yaml"),
		filepath.Join(filepath.Dir(wd), "config.yaml"),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func (s *Server) restartGatewayChannel(name string) (bool, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !isSupportedGatewayChannel(name) {
		return false, "channel is not supported"
	}
	return s.ensureGatewayChannelService().restart(name)
}

func (s *Server) stopGatewayChannels() {
	if s == nil {
		return
	}
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	if s.channelService != nil {
		s.channelService.stop()
	}
}

func (s *gatewayChannelService) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configPresent {
		s.running = false
		return
	}
	s.running = true
	for _, name := range supportedGatewayChannels {
		info := s.channels[name]
		info.Enabled = s.config.enabled(name)
		info.Running = info.Enabled
		s.channels[name] = info
	}
}

func (s *gatewayChannelService) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	for _, name := range supportedGatewayChannels {
		info := s.channels[name]
		info.Running = false
		s.channels[name] = info
	}
}

func (s *gatewayChannelService) snapshot() channelStatusSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := channelStatusSnapshot{
		ServiceRunning: s.running,
		Channels:       make(map[string]channelInfo, len(supportedGatewayChannels)),
	}
	for _, name := range supportedGatewayChannels {
		status.Channels[name] = s.channels[name]
	}
	return status
}

func (s *gatewayChannelService) restart(name string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configPresent {
		return false, "channel config.yaml was not found"
	}
	if !s.config.enabled(name) {
		return false, "channel is not enabled in config.yaml"
	}
	if !s.running {
		s.running = true
	}
	info := s.channels[name]
	info.Enabled = true
	info.Running = true
	s.channels[name] = info
	return true, "restarted successfully"
}

func isSupportedGatewayChannel(name string) bool {
	for _, candidate := range supportedGatewayChannels {
		if candidate == name {
			return true
		}
	}
	return false
}
