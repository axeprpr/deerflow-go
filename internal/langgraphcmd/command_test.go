package langgraphcmd

import (
	"strings"
	"testing"
)

func TestRunCommandRejectsWorkerRoleForLangGraph(t *testing.T) {
	err := RunCommand(nil, BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args:   []string{"-runtime-role=worker"},
	})
	if err == nil {
		t.Fatal("RunCommand() error = nil")
	}
	if !strings.Contains(err.Error(), "invalid runtime configuration") {
		t.Fatalf("RunCommand() error = %v", err)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "value", "later"); got != "value" {
		t.Fatalf("firstNonEmpty() = %q", got)
	}
}
