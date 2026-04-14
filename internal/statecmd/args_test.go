package statecmd

import "testing"

func TestCLIArgsOnlyIncludesStateCommandFlags(t *testing.T) {
	cfg := DefaultConfig()
	args := cfg.CLIArgs()
	joined := joinArgs(args)
	if containsArg(args, "-role=") {
		t.Fatalf("state CLI args contains unsupported role flag: %q", joined)
	}
	if containsArg(args, "-transport-backend=") {
		t.Fatalf("state CLI args contains unsupported transport-backend flag: %q", joined)
	}
	if !containsArg(args, "-state-provider=") {
		t.Fatalf("state CLI args missing state-provider flag: %q", joined)
	}
}

func containsArg(args []string, prefix string) bool {
	for _, arg := range args {
		if len(arg) >= len(prefix) && arg[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func joinArgs(args []string) string {
	out := ""
	for i, arg := range args {
		if i > 0 {
			out += " "
		}
		out += arg
	}
	return out
}
