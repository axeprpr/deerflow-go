package stackcmd

import (
	"flag"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
)

func TestPrepareCommandBuildsSplitReadyLines(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args:   []string{"-addr=:18080", "-worker-addr=:19081"},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
	if len(prepared.ReadyLines) < 2 {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
	if !strings.Contains(strings.Join(prepared.ReadyLines, "\n"), ":19081") {
		t.Fatalf("ReadyLines = %#v", prepared.ReadyLines)
	}
}

func TestPrepareCommandAcceptsSharedBackendFlags(t *testing.T) {
	prepared, err := PrepareCommand(flag.NewFlagSet("runtime-stack", flag.ContinueOnError), langgraphcmd.BuildInfo{}, CommandOptions{
		Stderr: new(strings.Builder),
		Args: []string{
			"-state-backend=sqlite",
			"-event-backend=file",
			"-thread-backend=sqlite",
			"-worker-transport=queue",
		},
	})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("PrepareCommand() = nil")
	}
}
