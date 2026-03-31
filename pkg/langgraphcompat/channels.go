package langgraphcompat

import (
	"os"
	"path/filepath"
	"strings"

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

func (s *Server) gatewayChannelStatus() channelStatusSnapshot {
	status := channelStatusSnapshot{
		ServiceRunning: false,
		Channels:       make(map[string]channelInfo, len(supportedGatewayChannels)),
	}
	for _, name := range supportedGatewayChannels {
		status.Channels[name] = channelInfo{}
	}

	config, ok := loadGatewayChannelConfig()
	if !ok {
		return status
	}
	for _, name := range supportedGatewayChannels {
		info := status.Channels[name]
		info.Enabled = config.enabled(name)
		status.Channels[name] = info
	}
	return status
}

type gatewayChannelsConfig struct {
	Channels map[string]map[string]any `yaml:"channels"`
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

	config, ok := loadGatewayChannelConfig()
	if !ok {
		return false, "channel config.yaml was not found"
	}
	if !config.enabled(name) {
		return false, "channel is not enabled in config.yaml"
	}
	return false, "channel runtime is not implemented in deerflow-go yet"
}

func isSupportedGatewayChannel(name string) bool {
	for _, candidate := range supportedGatewayChannels {
		if candidate == name {
			return true
		}
	}
	return false
}
