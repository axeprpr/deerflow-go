package sandboxcmd

import (
	"flag"
	"testing"
)

func TestPrepareCommandBuildsReadyLines(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-sandbox", flag.ContinueOnError), CommandOptions{
		Args: []string{"-addr=:19083"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if len(prepared.ReadyLines) != 1 || prepared.ReadyLines[0] != "runtime sandbox ready addr=:19083" {
		t.Fatalf("PrepareCommand().ReadyLines = %#v", prepared.ReadyLines)
	}
}
