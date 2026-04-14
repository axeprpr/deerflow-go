package stackcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewProcessLauncherRejectsMissingBinary(t *testing.T) {
	_, err := NewProcessLauncher([]ProcessManifest{
		{
			Name:   "gateway",
			Binary: "definitely-missing-runtime-binary",
		},
	}, ProcessLaunchOptions{})
	if err == nil {
		t.Fatal("NewProcessLauncher() error = nil, want missing executable")
	}
}

func installTestProcessBinaries(t *testing.T, names ...string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	dir := t.TempDir()
	for _, name := range names {
		dst := filepath.Join(dir, name)
		data, err := os.ReadFile(exe)
		if err != nil {
			t.Fatalf("read executable %s: %v", exe, err)
		}
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			t.Fatalf("write executable %s: %v", dst, err)
		}
	}
	return dir
}
