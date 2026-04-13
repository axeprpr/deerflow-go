package harnessruntime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeNodeConfigBuildRuntimeStateLauncher(t *testing.T) {
	config := DefaultWorkerRuntimeNodeConfig("runtime-state", t.TempDir())
	config.RemoteWorker.Addr = "127.0.0.1:49082"
	config.State.Backend = RuntimeStateStoreBackendInMemory
	config.State.SnapshotBackend = RuntimeStateStoreBackendInMemory
	config.State.EventBackend = RuntimeStateStoreBackendInMemory
	config.State.ThreadBackend = RuntimeStateStoreBackendInMemory

	launcher, err := config.BuildRuntimeStateLauncher()
	if err != nil {
		t.Fatalf("BuildRuntimeStateLauncher() error = %v", err)
	}
	if launcher.Spec().Addr != "127.0.0.1:49082" {
		t.Fatalf("Spec().Addr = %q", launcher.Spec().Addr)
	}

	server := httptest.NewServer(launcher.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + DefaultRemoteStateHealthPath)
	if err != nil {
		t.Fatalf("GET state health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state health status = %d", resp.StatusCode)
	}
}
