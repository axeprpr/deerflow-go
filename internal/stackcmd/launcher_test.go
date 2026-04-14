package stackcmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSplitStackLauncherStartsGatewayWorkerAndSandbox(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Gateway.Addr = freeTCPAddr(t)
	cfg.Worker.Addr = freeTCPAddr(t)
	cfg.Gateway.AuthToken = ""
	cfg.Gateway.DatabaseURL = ""
	cfg.Gateway.Runtime.DataRoot = t.TempDir()
	cfg.Worker.DataRoot = cfg.Gateway.Runtime.DataRoot

	launcher, err := cfg.BuildLauncher(context.Background())
	if err != nil {
		t.Fatalf("BuildLauncher() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- launcher.Start()
	}()
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-errCh:
		t.Fatalf("launcher.Start() early error = %v", err)
	default:
	}

	waitForHTTP(t, httpURL(cfg.Gateway.Addr)+"/api/models")
	waitForHTTP(t, httpURL(cfg.Worker.Addr)+"/health")
	waitForHTTP(t, httpURL(cfg.Worker.Addr)+"/sandbox/health")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launcher.Close(shutdownCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestSplitStackLauncherSharedRemoteStartsDedicatedStateAndSandbox(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Gateway.Addr = freeTCPAddr(t)
	cfg.Worker.Addr = freeTCPAddr(t)
	cfg.State.Runtime.Addr = freeTCPPortAddr(t)
	cfg.Sandbox.Runtime.Addr = freeTCPPortAddr(t)
	cfg.Gateway.AuthToken = ""
	cfg.Gateway.DatabaseURL = ""
	cfg.Gateway.Runtime.DataRoot = t.TempDir()
	cfg.Worker.DataRoot = cfg.Gateway.Runtime.DataRoot
	cfg.State.Runtime.Root = cfg.Worker.DataRoot

	launcher, err := cfg.BuildLauncher(context.Background())
	if err != nil {
		t.Fatalf("BuildLauncher() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- launcher.Start()
	}()

	manifest := launcher.Manifest()
	stateReady := componentReadyURL(manifest, ComponentState)
	sandboxReady := componentReadyURL(manifest, ComponentSandbox)
	workerReady := componentReadyURL(manifest, ComponentWorker)
	for name, target := range map[string]string{
		"state":   stateReady,
		"sandbox": sandboxReady,
		"worker":  workerReady,
	} {
		if strings.TrimSpace(target) == "" {
			t.Fatalf("missing %s ready target", name)
		}
	}

	waitForHTTP(t, stateReady)
	waitForHTTP(t, sandboxReady)
	waitForHTTP(t, workerReady)
	waitForHTTP(t, httpURL(cfg.Gateway.Addr)+"/api/models")
	waitForHTTP(t, httpURL(cfg.Worker.Addr)+"/health")
	for _, target := range manifest.ReadyTargets() {
		waitForHTTP(t, target)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launcher.Close(shutdownCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		t.Fatalf("Start() error = %v", err)
	}
}

func componentReadyURL(manifest StackManifest, kind ComponentKind) string {
	for _, component := range manifest.Components {
		if component.Kind == kind && strings.TrimSpace(component.ReadyURL) != "" {
			return component.ReadyURL
		}
	}
	return ""
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func freeTCPPortAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()
	return fmt.Sprintf(":%d", listener.Addr().(*net.TCPAddr).Port)
}

func waitForHTTP(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
			lastErr = fmt.Errorf("status=%d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("endpoint %s not ready: %v", endpoint, lastErr)
}

func httpURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	return "http://" + addr
}
