package harnessruntime

import (
	"context"
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
}
