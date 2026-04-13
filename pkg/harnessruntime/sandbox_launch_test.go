package harnessruntime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeNodeConfigBuildRuntimeSandboxLauncher(t *testing.T) {
	config := DefaultWorkerRuntimeNodeConfig("runtime-sandbox", t.TempDir())
	config.RemoteWorker.Addr = "127.0.0.1:49083"

	launcher, err := config.BuildRuntimeSandboxLauncher()
	if err != nil {
		t.Fatalf("BuildRuntimeSandboxLauncher() error = %v", err)
	}
	if launcher.Spec().Addr != "127.0.0.1:49083" {
		t.Fatalf("Spec().Addr = %q", launcher.Spec().Addr)
	}

	server := httptest.NewServer(launcher.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + DefaultRemoteSandboxHealthPath)
	if err != nil {
		t.Fatalf("GET sandbox health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sandbox health status = %d", resp.StatusCode)
	}
}
