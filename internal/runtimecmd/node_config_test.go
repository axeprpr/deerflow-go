package runtimecmd

import (
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestDefaultLangGraphNodeConfigUsesEnvironmentOverrides(t *testing.T) {
	t.Setenv("RUNTIME_NODE_ROLE", "gateway")
	t.Setenv("RUNTIME_NODE_ADDR", "9091")
	t.Setenv("RUNTIME_NODE_NAME", "edge")
	t.Setenv("RUNTIME_NODE_ROOT", "/tmp/runtime-root")
	t.Setenv("DEERFLOW_DATA_ROOT", "/tmp/data-root")
	t.Setenv("DEFAULT_LLM_PROVIDER", "openai")
	t.Setenv("RUNTIME_NODE_ENDPOINT", "http://worker:8081/dispatch")
	t.Setenv("RUNTIME_NODE_MAX_TURNS", "77")

	cfg := DefaultLangGraphNodeConfig()
	if cfg.Role != harnessruntime.RuntimeNodeRoleGateway {
		t.Fatalf("Role = %q", cfg.Role)
	}
	if cfg.Addr != ":9091" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.Name != "edge" {
		t.Fatalf("Name = %q", cfg.Name)
	}
	if cfg.Root != "/tmp/runtime-root" {
		t.Fatalf("Root = %q", cfg.Root)
	}
	if cfg.DataRoot != "/tmp/data-root" {
		t.Fatalf("DataRoot = %q", cfg.DataRoot)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("Provider = %q", cfg.Provider)
	}
	if cfg.Endpoint != "http://worker:8081/dispatch" {
		t.Fatalf("Endpoint = %q", cfg.Endpoint)
	}
	if cfg.MaxTurns != 77 {
		t.Fatalf("MaxTurns = %d", cfg.MaxTurns)
	}
}

func TestNodeConfigRuntimeNodeConfigUsesRoleDefaults(t *testing.T) {
	cfg := NodeConfig{
		Role:     harnessruntime.RuntimeNodeRoleGateway,
		Addr:     "9091",
		Name:     "edge",
		Root:     "/tmp/root",
		Endpoint: "http://worker:8081/dispatch",
	}
	node := cfg.RuntimeNodeConfig()
	if node.Role != harnessruntime.RuntimeNodeRoleGateway {
		t.Fatalf("Role = %q", node.Role)
	}
	if node.Transport.Backend != harnessruntime.WorkerTransportBackendRemote {
		t.Fatalf("Transport.Backend = %q", node.Transport.Backend)
	}
	if node.Transport.Endpoint != "http://worker:8081/dispatch" {
		t.Fatalf("Transport.Endpoint = %q", node.Transport.Endpoint)
	}
	if node.RemoteWorker.Addr != ":9091" {
		t.Fatalf("RemoteWorker.Addr = %q", node.RemoteWorker.Addr)
	}
}

func TestNodeConfigValidateForLangGraph(t *testing.T) {
	if err := (NodeConfig{Role: harnessruntime.RuntimeNodeRoleAllInOne}).ValidateForLangGraph(); err != nil {
		t.Fatalf("all-in-one validate error = %v", err)
	}
	if err := (NodeConfig{Role: harnessruntime.RuntimeNodeRoleGateway, Endpoint: "http://worker"}).ValidateForLangGraph(); err != nil {
		t.Fatalf("gateway validate error = %v", err)
	}
	if err := (NodeConfig{Role: harnessruntime.RuntimeNodeRoleGateway}).ValidateForLangGraph(); err == nil {
		t.Fatal("gateway without endpoint should fail")
	}
	if err := (NodeConfig{Role: harnessruntime.RuntimeNodeRoleWorker}).ValidateForLangGraph(); err == nil {
		t.Fatal("worker role should fail for langgraph")
	}
}
