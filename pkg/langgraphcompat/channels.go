package langgraphcompat

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/axeprpr/deerflow-go/pkg/reflection"
	"gopkg.in/yaml.v3"
)

var supportedGatewayChannels = []string{"feishu", "slack", "telegram"}

var gatewayChannelClassPaths = map[string]string{
	"feishu":   "app.channels.feishu:FeishuChannel",
	"slack":    "app.channels.slack:SlackChannel",
	"telegram": "app.channels.telegram:TelegramChannel",
}

type channelStatusSnapshot struct {
	ServiceRunning bool                   `json:"service_running"`
	Channels       map[string]channelInfo `json:"channels"`
}

type channelInfo struct {
	Enabled bool `json:"enabled"`
	Running bool `json:"running"`
}

type gatewayChannelStarter func(map[string]any) error

type gatewayChannelService struct {
	owner      *Server
	mu         sync.RWMutex
	configured bool
	config     gatewayChannelsConfig
	running    bool
	channels   map[string]channelInfo
	starters   map[string]gatewayChannelStarter
}

type gatewayChannelsConfig struct {
	Channels map[string]map[string]any `yaml:"channels"`
}

func newGatewayChannelService(owner *Server) *gatewayChannelService {
	svc := &gatewayChannelService{
		owner:    owner,
		channels: make(map[string]channelInfo, len(supportedGatewayChannels)),
		starters: defaultGatewayChannelStarters(),
	}
	for _, name := range supportedGatewayChannels {
		svc.channels[name] = channelInfo{}
	}
	svc.reloadLocked()
	return svc
}

func (s *Server) ensureGatewayChannelService() *gatewayChannelService {
	if s == nil {
		return newGatewayChannelService(nil)
	}
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	if s.channelService == nil {
		s.channelService = newGatewayChannelService(s)
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

func normalizeGatewayChannelsConfig(cfg gatewayChannelsConfig) gatewayChannelsConfig {
	if len(cfg.Channels) == 0 {
		return gatewayChannelsConfig{}
	}
	out := gatewayChannelsConfig{Channels: make(map[string]map[string]any, len(cfg.Channels))}
	for name, values := range cfg.Channels {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		cloned := make(map[string]any, len(values))
		for key, value := range values {
			cloned[key] = value
		}
		out.Channels[name] = cloned
	}
	if len(out.Channels) == 0 {
		return gatewayChannelsConfig{}
	}
	return out
}

func cloneGatewayChannelsConfig(cfg gatewayChannelsConfig) gatewayChannelsConfig {
	return normalizeGatewayChannelsConfig(cfg)
}

func (s *Server) loadGatewayChannelConfig() (gatewayChannelsConfig, bool) {
	if cfg, ok := loadGatewayChannelConfig(); ok {
		normalized := normalizeGatewayChannelsConfig(cfg)
		s.uiStateMu.Lock()
		s.channelConfig = normalized
		s.uiStateMu.Unlock()
		return normalized, true
	}
	if s == nil {
		return gatewayChannelsConfig{}, false
	}
	s.uiStateMu.RLock()
	defer s.uiStateMu.RUnlock()
	if len(s.channelConfig.Channels) == 0 {
		return gatewayChannelsConfig{}, false
	}
	return cloneGatewayChannelsConfig(s.channelConfig), true
}

func resolveGatewayConfigPath() (string, bool) {
	for _, key := range []string{"DEERFLOW_CONFIG_PATH", "DEER_FLOW_CONFIG_PATH"} {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path, true
			}
			return "", false
		}
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
		return 200, false, fmt.Sprintf("Failed to restart channel %s", name)
	}
	svc := s.ensureGatewayChannelService()
	if !svc.available() {
		return 503, false, "Channel service is not running"
	}
	success, _ := svc.restart(name)
	if success {
		return 200, true, fmt.Sprintf("Channel %s restarted successfully", name)
	}
	return 200, false, fmt.Sprintf("Failed to restart channel %s", name)
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
	s.startEnabledChannelsLocked()
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
		if s.running {
			s.startEnabledChannelsLocked()
		}
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
		return false, "channel is not enabled in config.yaml"
	}
	if err := s.startChannelLocked(name); err != nil {
		return false, err.Error()
	}
	return true, "restarted successfully"
}

func (s *gatewayChannelService) reloadLocked() {
	var (
		config gatewayChannelsConfig
		ok     bool
	)
	if s.owner != nil {
		config, ok = s.owner.loadGatewayChannelConfig()
	} else {
		config, ok = loadGatewayChannelConfig()
		if ok {
			config = normalizeGatewayChannelsConfig(config)
		}
	}
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
		info.Running = false
		s.channels[name] = info
	}
}

func (s *gatewayChannelService) startEnabledChannelsLocked() {
	for _, name := range supportedGatewayChannels {
		info := s.channels[name]
		if !s.running || !info.Enabled {
			info.Running = false
			s.channels[name] = info
			continue
		}
		if err := s.startChannelLocked(name); err != nil {
			info.Running = false
			s.channels[name] = info
		}
	}
}

func (s *gatewayChannelService) startChannelLocked(name string) error {
	info := s.channels[name]
	if !info.Enabled {
		info.Running = false
		s.channels[name] = info
		return fmt.Errorf("channel is not enabled in config.yaml")
	}
	starter, err := resolveGatewayChannelStarter(name, s.starters)
	if err != nil {
		info.Running = false
		s.channels[name] = info
		return err
	}
	cfg := s.config.Channels[name]
	if err := starter(cfg); err != nil {
		info.Running = false
		s.channels[name] = info
		return err
	}
	info.Running = s.running
	s.channels[name] = info
	return nil
}

func (s *gatewayChannelService) shouldReloadLocked() bool {
	var (
		config gatewayChannelsConfig
		ok     bool
	)
	if s.owner != nil {
		config, ok = s.owner.loadGatewayChannelConfig()
	} else {
		config, ok = loadGatewayChannelConfig()
		if ok {
			config = normalizeGatewayChannelsConfig(config)
		}
	}
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
		if !reflect.DeepEqual(s.config.Channels[name], config.Channels[name]) {
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

func defaultGatewayChannelStarters() map[string]gatewayChannelStarter {
	return map[string]gatewayChannelStarter{
		"feishu": func(cfg map[string]any) error {
			if strings.TrimSpace(stringConfigValue(cfg, "app_id")) == "" || strings.TrimSpace(stringConfigValue(cfg, "app_secret")) == "" {
				return fmt.Errorf("feishu channel requires app_id and app_secret")
			}
			return nil
		},
		"slack":    func(map[string]any) error { return nil },
		"telegram": func(map[string]any) error { return nil },
	}
}

func stringConfigValue(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	value, _ := cfg[key].(string)
	return value
}

func resolveGatewayChannelStarter(name string, starters map[string]gatewayChannelStarter) (gatewayChannelStarter, error) {
	classPath := strings.TrimSpace(gatewayChannelClassPaths[name])
	if classPath == "" {
		return nil, fmt.Errorf("Unknown channel type: %s", name)
	}
	return reflection.ResolveClass(classPath, func(modulePath, className string) reflection.SymbolLookupResult[gatewayChannelStarter] {
		path := strings.TrimSpace(modulePath) + ":" + strings.TrimSpace(className)
		for channelName, candidate := range gatewayChannelClassPaths {
			if candidate != path {
				continue
			}
			starter := starters[channelName]
			if starter == nil {
				return reflection.SymbolLookupResult[gatewayChannelStarter]{ModuleFound: true}
			}
			return reflection.SymbolLookupResult[gatewayChannelStarter]{
				Value:       starter,
				ModuleFound: true,
				SymbolFound: true,
			}
		}
		moduleFound := false
		prefix := strings.TrimSpace(modulePath) + ":"
		for _, candidate := range gatewayChannelClassPaths {
			if strings.HasPrefix(candidate, prefix) {
				moduleFound = true
				break
			}
		}
		return reflection.SymbolLookupResult[gatewayChannelStarter]{ModuleFound: moduleFound}
	})
}
