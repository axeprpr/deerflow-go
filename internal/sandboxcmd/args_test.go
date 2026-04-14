package sandboxcmd

import "testing"

func TestCLIArgsOnlyIncludesSandboxCommandFlags(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Runtime.SandboxMaxActiveLeases = 8
	args := cfg.CLIArgs()
	joined := joinArgs(args)
	if containsArg(args, "-role=") {
		t.Fatalf("sandbox CLI args contains unsupported role flag: %q", joined)
	}
	if containsArg(args, "-state-provider=") {
		t.Fatalf("sandbox CLI args contains unsupported state-provider flag: %q", joined)
	}
	if !containsArg(args, "-max-active-leases=") {
		t.Fatalf("sandbox CLI args missing max-active-leases flag: %q", joined)
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
