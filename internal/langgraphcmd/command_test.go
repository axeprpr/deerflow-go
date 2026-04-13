package langgraphcmd

import (
	"flag"
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

func TestPrepareCommandBuildsReadyLines(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("langgraph-prepare", flag.ContinueOnError), BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args:   []string{"-addr=:19080"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if len(prepared.ReadyLines) == 0 || !strings.Contains(strings.Join(prepared.ReadyLines, "\n"), ":19080") {
		t.Fatalf("PrepareCommand().ReadyLines = %#v", prepared.ReadyLines)
	}
	if prepared.Ready == nil {
		t.Fatal("PrepareCommand().Ready = nil")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "value", "later"); got != "value" {
		t.Fatalf("firstNonEmpty() = %q", got)
	}
}
