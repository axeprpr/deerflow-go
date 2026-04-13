package langgraphcmd

import (
	"flag"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

func TestBindFlagsBuildsConfig(t *testing.T) {
	fs := flag.NewFlagSet("langgraph", flag.ContinueOnError)
	binding := BindFlags(fs, DefaultConfig(":8080"))
	if err := fs.Parse([]string{
		"-addr=:9090",
		"-db=postgres://db",
		"-provider=openai",
		"-model=gpt-4.1-mini",
		"-runtime-role=gateway",
		"-runtime-endpoint=http://worker:8081/dispatch",
		"-runtime-transport-backend=remote",
	}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	cfg := binding.Config()
	if cfg.Addr != ":9090" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.DatabaseURL != "postgres://db" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("Provider = %q", cfg.Provider)
	}
	if cfg.Model != "gpt-4.1-mini" {
		t.Fatalf("Model = %q", cfg.Model)
	}
	if cfg.Runtime.Role != harnessruntime.RuntimeNodeRoleGateway {
		t.Fatalf("Runtime.Role = %q", cfg.Runtime.Role)
	}
	if cfg.Runtime.TransportBackend != harnessruntime.WorkerTransportBackendRemote {
		t.Fatalf("Runtime.TransportBackend = %q", cfg.Runtime.TransportBackend)
	}
}
