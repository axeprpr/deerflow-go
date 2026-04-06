package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDataRootFromEnvSupportsLegacyHomeEnv(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", "")
	t.Setenv("DEER_FLOW_HOME", "/tmp/deer-flow-home")

	if got := DataRootFromEnv(); got != "/tmp/deer-flow-home" {
		t.Fatalf("DataRootFromEnv()=%q want=%q", got, "/tmp/deer-flow-home")
	}
}

func TestDataRootFromEnvPrefersDedicatedDataRoot(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", "/tmp/deerflow-data-root")
	t.Setenv("DEER_FLOW_HOME", "/tmp/deer-flow-home")

	if got := DataRootFromEnv(); got != "/tmp/deerflow-data-root" {
		t.Fatalf("DataRootFromEnv()=%q want=%q", got, "/tmp/deerflow-data-root")
	}
}

func TestDataRootFromEnvFallsBackToTempDir(t *testing.T) {
	t.Setenv("DEERFLOW_DATA_ROOT", "")
	t.Setenv("DEER_FLOW_HOME", "")

	got := DataRootFromEnv()
	if got == "" {
		t.Fatal("expected non-empty data root")
	}
	if !strings.HasSuffix(filepath.Clean(got), filepath.Clean(filepath.Join("deerflow-go-data"))) {
		t.Fatalf("DataRootFromEnv()=%q want suffix deerflow-go-data", got)
	}
}
