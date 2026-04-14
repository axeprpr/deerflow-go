package stackcmd

import (
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

func (c Config) ReadyProbe() commandrun.ReadyFunc {
	targets := c.Manifest().ReadyTargets()
	filtered := make([]string, 0, len(targets))
	for _, target := range targets {
		if trimmed := strings.TrimSpace(target); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return commandrun.HTTPReadyProbe{
		Interval: 50 * time.Millisecond,
		Targets:  filtered,
	}.Wait
}
