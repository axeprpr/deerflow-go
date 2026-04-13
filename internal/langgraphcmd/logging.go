package langgraphcmd

import (
	"fmt"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func (c *Config) ApplyYoloDefaults(enabled bool) {
	if c == nil || !enabled {
		return
	}
	if c.Addr == "" || c.Addr == ":8080" {
		c.Addr = ":8080"
	}
	if c.Model == "" {
		c.Model = "qwen/Qwen3.5-9B"
	}
	if c.Provider == "" {
		c.Provider = "siliconflow"
	}
}

func (c Config) StartupLines(build BuildInfo, yolo bool, logLevel string) []string {
	lines := []string{
		"Starting deerflow-go server...",
		fmt.Sprintf("  YOLO mode: %v", yolo),
		fmt.Sprintf("  Address:   %s", c.Addr),
		fmt.Sprintf("  Database: %s", describeDB(c.DatabaseURL)),
		fmt.Sprintf("  Provider: %s", c.Provider),
		fmt.Sprintf("  Model:    %s", c.Model),
		fmt.Sprintf("  Runtime:  role=%s transport=%s worker_addr=%s worker_endpoint=%s sandbox=%s state=%s", c.Runtime.Role, c.Runtime.TransportBackend, c.Runtime.Addr, firstNonEmpty(c.Runtime.Endpoint, "(local)"), c.Runtime.SandboxBackend, firstNonEmpty(string(c.Runtime.StateBackend), "(default)")),
		fmt.Sprintf("  Auth:     %s", describeAuth(c.AuthToken, yolo)),
		fmt.Sprintf("  Version: %s (%s, %s)", build.Version, build.Commit, build.BuildTime),
	}
	if logLevel != "" {
		lines = append(lines, fmt.Sprintf("  Log Level: %s", logLevel))
	}
	return lines
}

func (c Config) ReadyLines() []string {
	lines := []string{
		fmt.Sprintf("Server ready on %s", c.Addr),
		fmt.Sprintf("  API docs: http://%s/docs", c.Addr),
	}
	switch c.Runtime.Role {
	case harnessruntime.RuntimeNodeRoleAllInOne:
		lines = append(lines, fmt.Sprintf("  Runtime worker: http://%s/dispatch", c.Runtime.Addr))
	case harnessruntime.RuntimeNodeRoleGateway:
		lines = append(lines, fmt.Sprintf("  Remote worker endpoint: %s", firstNonEmpty(c.Runtime.Endpoint, "(unset)")))
	}
	return lines
}
