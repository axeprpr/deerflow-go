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
	t.Setenv("RUNTIME_NODE_TRANSPORT_BACKEND", "remote")
	t.Setenv("RUNTIME_NODE_SANDBOX_BACKEND", "remote")
	t.Setenv("RUNTIME_NODE_SANDBOX_ENDPOINT", "http://sandbox:8082")
	t.Setenv("RUNTIME_NODE_STATE_BACKEND", "file")
	t.Setenv("RUNTIME_NODE_STATE_ROOT", "/tmp/state-root")

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
	if cfg.TransportBackend != harnessruntime.WorkerTransportBackendRemote {
		t.Fatalf("TransportBackend = %q", cfg.TransportBackend)
	}
	if cfg.SandboxBackend != harnessruntime.SandboxBackendRemote {
		t.Fatalf("SandboxBackend = %q", cfg.SandboxBackend)
	}
	if cfg.SandboxEndpoint != "http://sandbox:8082" {
		t.Fatalf("SandboxEndpoint = %q", cfg.SandboxEndpoint)
	}
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/state-root" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
}

func TestDefaultLangGraphNodeConfigUsesSharedSQLiteForGatewayRole(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", "/tmp/shared-data")
	t.Setenv("RUNTIME_NODE_ROLE", "gateway")
	t.Setenv("RUNTIME_NODE_ENDPOINT", "http://worker:8081/dispatch")

	cfg := DefaultLangGraphNodeConfig()
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/shared-data/runtime-state" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
}

func TestDefaultRuntimeWorkerNodeConfigUsesSharedSQLiteState(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", "/tmp/shared-data")

	cfg := DefaultRuntimeWorkerNodeConfig()
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/shared-data/runtime-state" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
}

func TestNormalizeStateBackendSupportsSQLite(t *testing.T) {
	if got := NormalizeStateBackend("sqlite", harnessruntime.RuntimeStateStoreBackendInMemory); got != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("NormalizeStateBackend() = %q", got)
	}
}

func TestNodeConfigRuntimeNodeConfigUsesRoleDefaults(t *testing.T) {
	cfg := NodeConfig{
		Role:             harnessruntime.RuntimeNodeRoleGateway,
		Addr:             "9091",
		Name:             "edge",
		Root:             "/tmp/root",
		Endpoint:         "http://worker:8081/dispatch",
		SandboxBackend:   harnessruntime.SandboxBackendContainer,
		SandboxImage:     "debian:bookworm",
		StateBackend:     harnessruntime.RuntimeStateStoreBackendFile,
		TransportBackend: harnessruntime.WorkerTransportBackendRemote,
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
	if node.Sandbox.Backend != harnessruntime.SandboxBackendContainer {
		t.Fatalf("Sandbox.Backend = %q", node.Sandbox.Backend)
	}
	if node.Sandbox.Image != "debian:bookworm" {
		t.Fatalf("Sandbox.Image = %q", node.Sandbox.Image)
	}
	if node.State.Root != "/tmp/root/state" {
		t.Fatalf("State.Root = %q", node.State.Root)
	}
	if node.State.Backend != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("State.Backend = %q", node.State.Backend)
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
	if err := (NodeConfig{Role: harnessruntime.RuntimeNodeRoleAllInOne, SandboxBackend: harnessruntime.SandboxBackendContainer}).ValidateForLangGraph(); err == nil {
		t.Fatal("container sandbox without image should fail")
	}
}
