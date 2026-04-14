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

func TestPrepareCommandPrintManifest(t *testing.T) {
	var stdout strings.Builder
	prepared, err := PrepareCommand(flag.NewFlagSet("langgraph-manifest", flag.ContinueOnError), BuildInfo{}, CommandOptions{
		Stdout: &stdout,
		Args:   []string{"-print-manifest", "-addr=:19080"},
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
	if !strings.Contains(stdout.String(), "\"addr\": \":19080\"") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"runtime\"") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "value", "later"); got != "value" {
		t.Fatalf("firstNonEmpty() = %q", got)
	}
}
