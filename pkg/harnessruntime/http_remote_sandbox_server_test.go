package harnessruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harness"
)

func TestHTTPRemoteSandboxServerHealthEndpoint(t *testing.T) {
	server := httptest.NewServer(NewHTTPRemoteSandboxServer(SandboxManagerConfig{
		Name: "sandbox-test",
		Root: t.TempDir(),
	}, nil).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + DefaultRemoteSandboxHealthPath)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body error = %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %#v", body)
	}
	if got, _ := body["active_leases"].(float64); got != 0 {
		t.Fatalf("active_leases = %v, want 0", got)
	}
	if got, _ := body["max_active_leases"].(float64); got != 0 {
		t.Fatalf("max_active_leases = %v, want 0", got)
	}
	if got, _ := body["oldest_lease_age_ms"].(float64); got != 0 {
		t.Fatalf("oldest_lease_age_ms = %v, want 0", got)
	}
	if got, _ := body["oldest_idle_age_ms"].(float64); got != 0 {
		t.Fatalf("oldest_idle_age_ms = %v, want 0", got)
	}
}

func TestRemoteSandboxLeaseServiceRoundTrip(t *testing.T) {
	server := httptest.NewServer(NewHTTPRemoteSandboxServer(SandboxManagerConfig{
		Name:              "sandbox-test",
		Root:              t.TempDir(),
		HeartbeatInterval: 25 * time.Millisecond,
	}, nil).Handler())
	defer server.Close()

	service := NewRemoteSandboxLeaseService(server.URL, nil)
	lease, err := service.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	if lease.Sandbox == nil {
		t.Fatal("AcquireLease().Sandbox = nil")
	}
	if got := lease.Sandbox.GetDir(); got == "" {
		t.Fatal("remote sandbox dir is empty")
	}
	if err := lease.Sandbox.WriteFile("notes/hello.txt", []byte("hello")); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	data, err := lease.Sandbox.ReadFile("notes/hello.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("ReadFile() = %q, want %q", string(data), "hello")
	}
	result, err := lease.Sandbox.Exec(context.Background(), "printf remote", time.Second)
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if got := result.Stdout(); got != "remote" {
		t.Fatalf("stdout = %q, want %q", got, "remote")
	}
	if lease.Heartbeat == nil || lease.Release == nil {
		t.Fatalf("lease callbacks = %#v", lease)
	}
	if err := lease.Heartbeat(); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := lease.Sandbox.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	resp, err := http.Get(server.URL + DefaultRemoteSandboxHealthPath)
	if err != nil {
		t.Fatalf("health get error = %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode health body error = %v", err)
	}
	if got, _ := body["oldest_lease_age_ms"].(float64); got < 0 {
		t.Fatalf("oldest_lease_age_ms = %v, want >= 0", got)
	}
	if got, _ := body["oldest_idle_age_ms"].(float64); got < 0 {
		t.Fatalf("oldest_idle_age_ms = %v, want >= 0", got)
	}
}

func TestHTTPRemoteSandboxServerEvictsIdleLeases(t *testing.T) {
	httpServer := NewHTTPRemoteSandboxServer(SandboxManagerConfig{
		Name:              "sandbox-evict",
		Root:              t.TempDir(),
		HeartbeatInterval: 10 * time.Millisecond,
		IdleTTL:           40 * time.Millisecond,
		SweepInterval:     10 * time.Millisecond,
	}, nil)
	server := httptest.NewServer(httpServer.Handler())
	defer server.Close()
	defer func() {
		_ = httpServer.Close(context.Background())
	}()

	service := NewRemoteSandboxLeaseService(server.URL, nil)
	lease, err := service.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	if lease.Sandbox == nil {
		t.Fatal("AcquireLease().Sandbox = nil")
	}

	deadline := time.Now().Add(2 * time.Second)
	evicted := false
	for time.Now().Before(deadline) {
		resp, err := http.Get(server.URL + DefaultRemoteSandboxHealthPath)
		if err != nil {
			t.Fatalf("health get error = %v", err)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			resp.Body.Close()
			t.Fatalf("decode health body error = %v", err)
		}
		resp.Body.Close()
		activeLeases, _ := body["active_leases"].(float64)
		evictedLeases, _ := body["evicted_leases"].(float64)
		if activeLeases == 0 && evictedLeases >= 1 {
			evicted = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !evicted {
		t.Fatal("timed out waiting for idle lease eviction")
	}

	if err := lease.Heartbeat(); err == nil {
		t.Fatal("Heartbeat() error = nil after idle eviction, want not found")
	}
}

func TestHTTPRemoteSandboxServerEnforcesMaxActiveLeases(t *testing.T) {
	httpServer := NewHTTPRemoteSandboxServer(SandboxManagerConfig{
		Name:            "sandbox-limit",
		Root:            t.TempDir(),
		MaxActiveLeases: 1,
	}, nil)
	server := httptest.NewServer(httpServer.Handler())
	defer server.Close()
	defer func() {
		_ = httpServer.Close(context.Background())
	}()

	service := NewRemoteSandboxLeaseService(server.URL, nil)
	first, err := service.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("first AcquireLease() error = %v", err)
	}
	if _, err := service.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}}); err == nil {
		t.Fatal("second AcquireLease() error = nil, want 429 limit")
	}
	if err := first.Release(); err != nil {
		t.Fatalf("first Release() error = %v", err)
	}
	second, err := service.AcquireLease(harness.AgentRequest{Features: harness.FeatureSet{Sandbox: true}})
	if err != nil {
		t.Fatalf("second AcquireLease() after release error = %v", err)
	}
	if second.Sandbox == nil {
		t.Fatal("second lease sandbox is nil")
	}
	if err := second.Release(); err != nil {
		t.Fatalf("second Release() error = %v", err)
	}
}
