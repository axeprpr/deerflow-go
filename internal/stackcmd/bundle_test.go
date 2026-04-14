package stackcmd

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

func TestWriteBundleWritesManifestAndScripts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preset = StackPresetSharedRemote
	cfg.Worker.Addr = ":19081"

	dir := t.TempDir()
	if err := WriteBundle(dir, cfg.Manifest()); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stack-manifest.json")); err != nil {
		t.Fatalf("manifest stat error = %v", err)
	}
	for _, name := range []string{"gateway.sh", "worker.sh", "state.sh", "sandbox.sh"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("script %s read error = %v", name, err)
		}
		if !strings.Contains(string(data), "exec ") {
			t.Fatalf("script %s = %q", name, string(data))
		}
	}
}

func TestPrepareCommandWriteBundle(t *testing.T) {
	var stdout strings.Builder
	dir := t.TempDir()
	prepared, err := PrepareCommand(flagSet("runtime-stack-bundle"), langgraphcmd.BuildInfo{}, CommandOptions{
		Stdout: &stdout,
		Args:   []string{"-write-bundle=" + dir, "-preset=shared-remote", "-worker-addr=:19081"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil || prepared.RunFunc == nil {
		t.Fatal("PrepareCommand() did not build bundle run func")
	}
	if err := prepared.Run(); err != nil {
		t.Fatalf("prepared.Run() error = %v", err)
	}
	if strings.TrimSpace(stdout.String()) != dir {
		t.Fatalf("stdout = %q, want %q", stdout.String(), dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "stack-manifest.json")); err != nil {
		t.Fatalf("manifest stat error = %v", err)
	}
}

func flagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}
