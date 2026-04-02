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
	configured    bool
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
	svc.reloadLocked()
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
	if len(cfg.Channels) == 0 {
		return gatewayChannelsConfig{}, false
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

func (s *Server) restartGatewayChannel(name string) (int, bool, string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !isSupportedGatewayChannel(name) {
		return 200, false, "channel is not supported"
	}
	svc := s.ensureGatewayChannelService()
	if !svc.available() {
		return 503, false, "channel service is not running"
	}
	success, message := svc.restart(name)
	return 200, success, message
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
	s.reloadLocked()
	s.running = s.configured
	s.syncRunningStateLocked()
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
	needsReload := s.shouldReloadLocked()
	if !needsReload {
		snap := s.snapshotLocked()
		s.mu.RUnlock()
		return snap
	}
	s.mu.RUnlock()
	if needsReload {
		s.mu.Lock()
		s.reloadLocked()
		s.syncRunningStateLocked()
		snap := s.snapshotLocked()
		s.mu.Unlock()
		return snap
	}
	return channelStatusSnapshot{}
}

func (s *gatewayChannelService) snapshotLocked() channelStatusSnapshot {
	if !s.configured {
		return channelStatusSnapshot{
			ServiceRunning: false,
			Channels:       map[string]channelInfo{},
		}
	}
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
	s.reloadLocked()
	if !s.configured {
		return false, "channel service is not running"
	}
	if !s.config.enabled(name) {
		info := s.channels[name]
		info.Enabled = false
		info.Running = false
		s.channels[name] = info
		s.syncRunningStateLocked()
		return false, "channel is not enabled in config.yaml"
	}
	info := s.channels[name]
	info.Enabled = true
	info.Running = true
	s.channels[name] = info
	s.syncRunningStateLocked()
	return true, "restarted successfully"
}

func (s *gatewayChannelService) reloadLocked() {
	config, ok := loadGatewayChannelConfig()
	s.configured = ok
	if ok {
		s.config = config
	} else {
		s.config = gatewayChannelsConfig{}
		s.running = false
	}
	for _, name := range supportedGatewayChannels {
		info := s.channels[name]
		enabled := ok && config.enabled(name)
		info.Enabled = enabled
		if !enabled {
			info.Running = false
		}
		s.channels[name] = info
	}
}

func (s *gatewayChannelService) syncRunningStateLocked() {
	for _, name := range supportedGatewayChannels {
		info := s.channels[name]
		info.Running = s.running && info.Enabled
		s.channels[name] = info
	}
}

func (s *gatewayChannelService) shouldReloadLocked() bool {
	config, ok := loadGatewayChannelConfig()
	if ok != s.configured {
		return true
	}
	if !ok {
		return false
	}
	for _, name := range supportedGatewayChannels {
		if s.config.enabled(name) != config.enabled(name) {
			return true
		}
	}
	return false
}

func (s *gatewayChannelService) available() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.configured
}

func isSupportedGatewayChannel(name string) bool {
	for _, candidate := range supportedGatewayChannels {
		if candidate == name {
			return true
		}
	}
	return false
}
