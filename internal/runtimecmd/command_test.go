package runtimecmd

import (
	"context"
	"flag"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestRunCommandRejectsGatewayRole(t *testing.T) {
	if err := RunCommand(nil, CommandOptions{Args: []string{"-role=gateway"}}); err == nil {
		t.Fatal("RunCommand() error = nil")
	}
}

func TestIsExpectedCommandExit(t *testing.T) {
	if !IsExpectedCommandExit(nil) {
		t.Fatal("nil should be treated as expected exit")
	}
	if !IsExpectedCommandExit(http.ErrServerClosed) {
		t.Fatal("http.ErrServerClosed should be treated as expected exit")
	}
	if !IsExpectedCommandExit(context.Canceled) {
		t.Fatal("context.Canceled should be treated as expected exit")
	}
	if IsExpectedCommandExit(os.ErrNotExist) {
		t.Fatal("unexpected errors should not be treated as expected exit")
	}
}

func TestPrepareCommandBuildsReadyLines(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-prepare", flag.ContinueOnError), CommandOptions{
		Stderr: new(strings.Builder),
		Args:   []string{"-role=worker", "-addr=:19081"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if len(prepared.ReadyLines) != 1 || !strings.Contains(prepared.ReadyLines[0], ":19081") {
		t.Fatalf("PrepareCommand().ReadyLines = %#v", prepared.ReadyLines)
	}
	if prepared.Ready == nil {
		t.Fatal("PrepareCommand().Ready = nil")
	}
}

func TestPrepareCommandPrintManifest(t *testing.T) {
	var stdout strings.Builder
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-manifest", flag.ContinueOnError), CommandOptions{
		Stdout: &stdout,
		Args:   []string{"-print-manifest", "-role=worker", "-addr=:19081"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil || prepared.RunFunc == nil {
		t.Fatal("PrepareCommand() did not build manifest run func")
	}
	if err := prepared.Run(); err != nil {
		t.Fatalf("prepared.Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"role\": \"worker\"") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"addr\": \":19081\"") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
