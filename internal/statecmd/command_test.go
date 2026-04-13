package statecmd

import (
	"flag"
	"testing"
)

func TestPrepareCommandBuildsReadyLines(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-state", flag.ContinueOnError), CommandOptions{
		Args: []string{"-addr=:19082"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if len(prepared.ReadyLines) != 1 || prepared.ReadyLines[0] != "runtime state ready addr=:19082" {
		t.Fatalf("PrepareCommand().ReadyLines = %#v", prepared.ReadyLines)
	}
	if prepared.Ready == nil {
		t.Fatal("PrepareCommand().Ready = nil")
	}
}
