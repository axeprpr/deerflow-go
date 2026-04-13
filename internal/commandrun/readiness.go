package commandrun

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ReadyFunc func(context.Context) error

type HTTPReadyProbe struct {
	Client   *http.Client
	Interval time.Duration
	Targets  []string
}

func (p HTTPReadyProbe) Wait(ctx context.Context) error {
	targets := make([]string, 0, len(p.Targets))
	for _, target := range p.Targets {
		if trimmed := strings.TrimSpace(target); trimmed != "" {
			targets = append(targets, trimmed)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 500 * time.Millisecond}
	}
	interval := p.Interval
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for _, target := range targets {
		for {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				return fmt.Errorf("invalid readiness target %q: %w", target, err)
			}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode < http.StatusInternalServerError {
					break
				}
				err = fmt.Errorf("status=%d", resp.StatusCode)
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("readiness probe %q failed: %w", target, ctx.Err())
			case <-ticker.C:
			}
			_ = err
		}
	}
	return nil
}

func WaitHTTPReady(targets ...string) ReadyFunc {
	probe := HTTPReadyProbe{Targets: targets}
	return probe.Wait
}

func HTTPURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	return "http://" + addr
}
