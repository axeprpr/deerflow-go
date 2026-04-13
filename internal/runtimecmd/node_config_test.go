package runtimecmd

import (
	"strings"
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
	t.Setenv("RUNTIME_NODE_MEMORY_STORE", "sqlite:///tmp/memory.sqlite3")
	t.Setenv("RUNTIME_NODE_STATE_STORE", "sqlite:///tmp/runtime.sqlite3")
	t.Setenv("RUNTIME_NODE_SNAPSHOT_STORE", "sqlite:///tmp/snapshots.sqlite3")
	t.Setenv("RUNTIME_NODE_EVENT_STORE", "sqlite:///tmp/events.sqlite3")
	t.Setenv("RUNTIME_NODE_THREAD_STORE", "sqlite:///tmp/threads.sqlite3")
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
	if cfg.MemoryStoreURL != "sqlite:///tmp/memory.sqlite3" {
		t.Fatalf("MemoryStoreURL = %q", cfg.MemoryStoreURL)
	}
	if cfg.StateStoreURL != "sqlite:///tmp/runtime.sqlite3" {
		t.Fatalf("StateStoreURL = %q", cfg.StateStoreURL)
	}
	if cfg.SnapshotStoreURL != "sqlite:///tmp/snapshots.sqlite3" {
		t.Fatalf("SnapshotStoreURL = %q", cfg.SnapshotStoreURL)
	}
	if cfg.EventStoreURL != "sqlite:///tmp/events.sqlite3" {
		t.Fatalf("EventStoreURL = %q", cfg.EventStoreURL)
	}
	if cfg.ThreadStoreURL != "sqlite:///tmp/threads.sqlite3" {
		t.Fatalf("ThreadStoreURL = %q", cfg.ThreadStoreURL)
	}
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/state-root" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
}

func TestNodeConfigDerivesSharedStateBackendFromURLOverExplicitBackend(t *testing.T) {
	cfg := NodeConfig{
		Role:          harnessruntime.RuntimeNodeRoleWorker,
		StateBackend:  harnessruntime.RuntimeStateStoreBackendFile,
		StateStoreURL: "sqlite:///tmp/runtime.sqlite3",
	}.withRoleDefaults()
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
}

func TestDefaultLangGraphNodeConfigUsesSharedSQLiteForGatewayRole(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", "/tmp/shared-data")
	t.Setenv("RUNTIME_NODE_ROLE", "gateway")
	t.Setenv("RUNTIME_NODE_ENDPOINT", "http://worker:8081/dispatch")

	cfg := DefaultLangGraphNodeConfig()
	if cfg.Preset != RuntimeNodePresetAuto {
		t.Fatalf("Preset = %q", cfg.Preset)
	}
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/shared-data/runtime-state" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
	if cfg.MemoryStoreURL != "sqlite:///tmp/shared-data/memory.sqlite3" {
		t.Fatalf("MemoryStoreURL = %q", cfg.MemoryStoreURL)
	}
	if cfg.StateStoreURL != "sqlite:///tmp/shared-data/runtime-state/runtime.sqlite3" {
		t.Fatalf("StateStoreURL = %q", cfg.StateStoreURL)
	}
	if cfg.SnapshotStoreURL != cfg.StateStoreURL {
		t.Fatalf("SnapshotStoreURL = %q", cfg.SnapshotStoreURL)
	}
	if cfg.EventStoreURL != cfg.StateStoreURL {
		t.Fatalf("EventStoreURL = %q", cfg.EventStoreURL)
	}
	if cfg.ThreadStoreURL != cfg.StateStoreURL {
		t.Fatalf("ThreadStoreURL = %q", cfg.ThreadStoreURL)
	}
}

func TestDefaultRuntimeWorkerNodeConfigUsesSharedSQLiteState(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", "/tmp/shared-data")

	cfg := DefaultRuntimeWorkerNodeConfig()
	if cfg.Preset != RuntimeNodePresetAuto {
		t.Fatalf("Preset = %q", cfg.Preset)
	}
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateRoot != "/tmp/shared-data/runtime-state" {
		t.Fatalf("StateRoot = %q", cfg.StateRoot)
	}
	if cfg.MemoryStoreURL != "sqlite:///tmp/shared-data/memory.sqlite3" {
		t.Fatalf("MemoryStoreURL = %q", cfg.MemoryStoreURL)
	}
	if cfg.StateStoreURL != "sqlite:///tmp/shared-data/runtime-state/runtime.sqlite3" {
		t.Fatalf("StateStoreURL = %q", cfg.StateStoreURL)
	}
	if cfg.SnapshotStoreURL != cfg.StateStoreURL {
		t.Fatalf("SnapshotStoreURL = %q", cfg.SnapshotStoreURL)
	}
	if cfg.EventStoreURL != cfg.StateStoreURL {
		t.Fatalf("EventStoreURL = %q", cfg.EventStoreURL)
	}
	if cfg.ThreadStoreURL != cfg.StateStoreURL {
		t.Fatalf("ThreadStoreURL = %q", cfg.ThreadStoreURL)
	}
}

func TestDefaultNodeConfigForRoleUsesRoleSpecificNames(t *testing.T) {
	if cfg := DefaultNodeConfigForRole(harnessruntime.RuntimeNodeRoleAllInOne); cfg.Name != "langgraph" {
		t.Fatalf("all-in-one Name = %q", cfg.Name)
	}
	if cfg := DefaultNodeConfigForRole(harnessruntime.RuntimeNodeRoleGateway); cfg.Name != "langgraph-gateway" {
		t.Fatalf("gateway Name = %q", cfg.Name)
	}
	if cfg := DefaultNodeConfigForRole(harnessruntime.RuntimeNodeRoleWorker); cfg.Name != "runtime-node" {
		t.Fatalf("worker Name = %q", cfg.Name)
	}
}

func TestDefaultSplitNodeConfigsUseSharedSQLitePreset(t *testing.T) {
	gateway := DefaultSplitGatewayNodeConfig("http://worker:8081/dispatch")
	if gateway.Preset != RuntimeNodePresetSharedSQLite {
		t.Fatalf("gateway Preset = %q", gateway.Preset)
	}
	if gateway.StateProvider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("gateway StateProvider = %q", gateway.StateProvider)
	}
	if gateway.Endpoint != "http://worker:8081/dispatch" {
		t.Fatalf("gateway Endpoint = %q", gateway.Endpoint)
	}

	worker := DefaultSplitWorkerNodeConfig()
	if worker.Preset != RuntimeNodePresetSharedSQLite {
		t.Fatalf("worker Preset = %q", worker.Preset)
	}
	if worker.StateProvider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("worker StateProvider = %q", worker.StateProvider)
	}
}

func TestNormalizeStateBackendSupportsSQLite(t *testing.T) {
	if got := NormalizeStateBackend("sqlite", harnessruntime.RuntimeStateStoreBackendInMemory); got != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("NormalizeStateBackend() = %q", got)
	}
}

func TestNormalizeStateProviderSupportsKnownValues(t *testing.T) {
	if got := NormalizeStateProvider("shared-sqlite", harnessruntime.RuntimeStateProviderModeAuto); got != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("NormalizeStateProvider() = %q", got)
	}
	if got := NormalizeStateProvider("isolated", harnessruntime.RuntimeStateProviderModeAuto); got != harnessruntime.RuntimeStateProviderModeIsolated {
		t.Fatalf("NormalizeStateProvider() = %q", got)
	}
}

func TestDeriveStateBackendFromStoreURL(t *testing.T) {
	if got := deriveStateBackendFromStoreURL("sqlite:///tmp/runtime.sqlite3", harnessruntime.RuntimeStateStoreBackendInMemory); got != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("deriveStateBackendFromStoreURL(sqlite) = %q", got)
	}
	if got := deriveStateBackendFromStoreURL("file:///tmp/runs", harnessruntime.RuntimeStateStoreBackendInMemory); got != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("deriveStateBackendFromStoreURL(file) = %q", got)
	}
}

func TestNormalizePresetSupportsKnownValues(t *testing.T) {
	if got := NormalizePreset("shared-sqlite", RuntimeNodePresetAuto); got != RuntimeNodePresetSharedSQLite {
		t.Fatalf("NormalizePreset() = %q", got)
	}
	if got := NormalizePreset("fast-local", RuntimeNodePresetAuto); got != RuntimeNodePresetFastLocal {
		t.Fatalf("NormalizePreset() = %q", got)
	}
}

func TestFastLocalPresetKeepsWorkerOnFastPath(t *testing.T) {
	cfg := NodeConfig{
		Preset:   RuntimeNodePresetFastLocal,
		Role:     harnessruntime.RuntimeNodeRoleWorker,
		DataRoot: "/tmp/shared-data",
	}.withRoleDefaults()
	if cfg.StateBackend != "" && cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendInMemory {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateStoreURL != "" {
		t.Fatalf("StateStoreURL = %q", cfg.StateStoreURL)
	}
	if cfg.MemoryStoreURL != "" {
		t.Fatalf("MemoryStoreURL = %q", cfg.MemoryStoreURL)
	}
	if cfg.StateProvider != harnessruntime.RuntimeStateProviderModeIsolated {
		t.Fatalf("StateProvider = %q", cfg.StateProvider)
	}
}

func TestSharedSQLitePresetPromotesAllInOneToSharedState(t *testing.T) {
	cfg := NodeConfig{
		Preset:   RuntimeNodePresetSharedSQLite,
		Role:     harnessruntime.RuntimeNodeRoleAllInOne,
		DataRoot: "/tmp/shared-data",
	}.withRoleDefaults()
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.StateStoreURL == "" {
		t.Fatal("StateStoreURL = empty")
	}
	if cfg.MemoryStoreURL == "" {
		t.Fatal("MemoryStoreURL = empty")
	}
	if cfg.StateProvider != harnessruntime.RuntimeStateProviderModeSharedSQLite {
		t.Fatalf("StateProvider = %q", cfg.StateProvider)
	}
}

func TestNodeConfigDerivesBackendsFromStoreURLs(t *testing.T) {
	cfg := NodeConfig{
		Role:             harnessruntime.RuntimeNodeRoleAllInOne,
		StateBackend:     harnessruntime.RuntimeStateStoreBackendInMemory,
		StateStoreURL:    "sqlite:///tmp/runtime.sqlite3",
		SnapshotStoreURL: "file:///tmp/snapshots",
		EventStoreURL:    "sqlite:///tmp/events.sqlite3",
		ThreadStoreURL:   "file:///tmp/threads",
	}.withRoleDefaults()
	if cfg.StateBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("StateBackend = %q", cfg.StateBackend)
	}
	if cfg.SnapshotBackend != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("SnapshotBackend = %q", cfg.SnapshotBackend)
	}
	if cfg.EventBackend != harnessruntime.RuntimeStateStoreBackendSQLite {
		t.Fatalf("EventBackend = %q", cfg.EventBackend)
	}
	if cfg.ThreadBackend != harnessruntime.RuntimeStateStoreBackendFile {
		t.Fatalf("ThreadBackend = %q", cfg.ThreadBackend)
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
		StateStoreURL:    "sqlite:///tmp/runtime.sqlite3",
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
	if node.Memory.StoreURL != "" {
		t.Fatalf("Memory.StoreURL = %q, want empty", node.Memory.StoreURL)
	}
	if node.State.URL != "sqlite:///tmp/runtime.sqlite3" {
		t.Fatalf("State.URL = %q", node.State.URL)
	}
	if node.State.SnapshotURL != "" || node.State.EventURL != "" || node.State.ThreadURL != "" {
		t.Fatalf("state urls = %+v", node.State)
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

func TestNodeConfigValidateRejectsConflictingSharedStateStores(t *testing.T) {
	cfg := NodeConfig{
		Role:          harnessruntime.RuntimeNodeRoleWorker,
		StateBackend:  harnessruntime.RuntimeStateStoreBackendSQLite,
		StateStoreURL: "sqlite:///tmp/runtime.sqlite3",
		EventStoreURL: "sqlite:///tmp/events.sqlite3",
	}
	if err := cfg.ValidateForRuntimeNode(); err == nil || !strings.Contains(err.Error(), "event-store must match state-store") {
		t.Fatalf("ValidateForRuntimeNode() error = %v", err)
	}
}

func TestNodeConfigValidateRejectsSharedStateStoreWithoutSQLiteBackends(t *testing.T) {
	cfg := NodeConfig{
		Role:          harnessruntime.RuntimeNodeRoleWorker,
		StateBackend:  harnessruntime.RuntimeStateStoreBackendFile,
		StateStoreURL: "sqlite:///tmp/runtime.sqlite3",
	}
	if err := cfg.ValidateForRuntimeNode(); err == nil || !strings.Contains(err.Error(), "state-store requires sqlite") {
		t.Fatalf("ValidateForRuntimeNode() error = %v", err)
	}
}
