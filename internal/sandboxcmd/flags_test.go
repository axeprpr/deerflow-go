package sandboxcmd

import (
	"flag"
	"testing"
)

func TestBindFlagsSupportsMaxActiveLeases(t *testing.T) {
	fs := flag.NewFlagSet("runtime-sandbox-flags", flag.ContinueOnError)
	binding := BindFlags(fs, DefaultConfig())
	if err := fs.Parse([]string{"-max-active-leases=9"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	cfg := binding.Config()
	if cfg.Runtime.SandboxMaxActiveLeases != 9 {
		t.Fatalf("SandboxMaxActiveLeases = %d", cfg.Runtime.SandboxMaxActiveLeases)
	}
}

func TestBindFlagsWithPrefixSupportsMaxActiveLeases(t *testing.T) {
	fs := flag.NewFlagSet("runtime-sandbox-prefixed-flags", flag.ContinueOnError)
	binding := BindFlagsWithPrefix(fs, DefaultConfig(), "sandbox-service-", "sandbox service ")
	if err := fs.Parse([]string{"-sandbox-service-max-active-leases=5"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	cfg := binding.Config()
	if cfg.Runtime.SandboxMaxActiveLeases != 5 {
		t.Fatalf("SandboxMaxActiveLeases = %d", cfg.Runtime.SandboxMaxActiveLeases)
	}
}
