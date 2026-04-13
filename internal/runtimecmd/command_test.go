package runtimecmd

import (
	"context"
	"net/http"
	"os"
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
