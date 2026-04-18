package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandboxCreate(t *testing.T) {
	baseDir := t.TempDir()
	sb, err := New("create", baseDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer sb.Close()

	info, err := os.Stat(sb.GetDir())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", sb.GetDir())
	}
}

func TestSandboxExec(t *testing.T) {
	baseDir := t.TempDir()
	sb, err := New("exec", baseDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), "echo hello", 5*time.Second)
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if got := strings.TrimSpace(result.Stdout()); got != "hello" {
		t.Fatalf("stdout = %q, want hello", got)
	}
	if result.ExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode())
	}
}

func TestSandboxWriteRead(t *testing.T) {
	baseDir := t.TempDir()
	sb, err := New("files", baseDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer sb.Close()

	path := filepath.Join("nested", "message.txt")
	want := []byte("sandbox data")
	if err := sb.WriteFile(path, want); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := sb.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("ReadFile() = %q, want %q", string(got), string(want))
	}
}

func TestSandboxTimeout(t *testing.T) {
	baseDir := t.TempDir()
	sb, err := New("timeout", baseDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), "sleep 2", 200*time.Millisecond)
	if err == nil {
		t.Fatal("Exec() error = nil, want timeout")
	}
	if result == nil {
		t.Fatal("Exec() result = nil")
	}
	if result.Error() == nil {
		t.Fatal("result.Error() = nil, want timeout")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Exec() error = %T, want *TimeoutError", err)
	}
}

func TestSandboxWindowsRestrictedCommandPolicyAllowsConfiguredCommand(t *testing.T) {
	baseDir := t.TempDir()
	sb, err := NewWithConfig("restricted-allow", baseDir, Config{
		ExecutionBackend: ExecutionBackendWindowsRestricted,
		AllowedCommands:  []string{"echo"},
	})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), "echo hello", 5*time.Second)
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if got := strings.TrimSpace(result.Stdout()); got != "hello" {
		t.Fatalf("stdout = %q, want hello", got)
	}
}

func TestSandboxWindowsRestrictedCommandPolicyRejectsDisallowedCommand(t *testing.T) {
	baseDir := t.TempDir()
	sb, err := NewWithConfig("restricted-deny", baseDir, Config{
		ExecutionBackend: ExecutionBackendWindowsRestricted,
		AllowedCommands:  []string{"echo"},
	})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	defer sb.Close()

	if _, err := sb.Exec(context.Background(), "python --version", 5*time.Second); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Exec() error = %v", err)
	}
}
