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
}
