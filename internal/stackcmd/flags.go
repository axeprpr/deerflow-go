package stackcmd

import (
	"flag"

	"github.com/axeprpr/deerflow-go/internal/statecmd"
)

type Binding struct {
	cfg          *Config
	stateBinding *statecmd.Binding
}

func BindFlags(fs *flag.FlagSet, defaults Config) *Binding {
	cfg := defaults
	fs.StringVar((*string)(&cfg.Preset), "preset", string(cfg.Preset), "runtime stack preset: auto|shared-sqlite|shared-remote")
	fs.StringVar(&cfg.Gateway.Addr, "addr", cfg.Gateway.Addr, "gateway HTTP listen address")
	fs.StringVar(&cfg.Worker.Addr, "worker-addr", cfg.Worker.Addr, "worker remote service address")
	fs.StringVar(&cfg.Gateway.DatabaseURL, "db", cfg.Gateway.DatabaseURL, "gateway database URL")
	fs.StringVar(&cfg.Gateway.AuthToken, "auth-token", cfg.Gateway.AuthToken, "gateway auth token")
	fs.StringVar(&cfg.Gateway.Provider, "provider", cfg.Gateway.Provider, "LLM provider")
	fs.StringVar(&cfg.Gateway.Model, "model", cfg.Gateway.Model, "default model")
	fs.StringVar(&cfg.Gateway.Runtime.DataRoot, "data-root", cfg.Gateway.Runtime.DataRoot, "shared data root")
	fs.IntVar(&cfg.Gateway.Runtime.MaxTurns, "max-turns", cfg.Gateway.Runtime.MaxTurns, "max agent turns")
	fs.StringVar(&cfg.Gateway.Runtime.MemoryStoreURL, "memory-store", cfg.Gateway.Runtime.MemoryStoreURL, "shared memory store URL")
	fs.StringVar(&cfg.Gateway.Runtime.StateRoot, "state-root", cfg.Gateway.Runtime.StateRoot, "shared runtime state root")
	fs.StringVar((*string)(&cfg.Gateway.Runtime.StateProvider), "state-provider", string(cfg.Gateway.Runtime.StateProvider), "shared runtime state provider: auto|isolated|shared-sqlite")
	fs.StringVar(&cfg.Gateway.Runtime.StateStoreURL, "state-store", cfg.Gateway.Runtime.StateStoreURL, "shared runtime state store URL")
	fs.StringVar((*string)(&cfg.Worker.TransportBackend), "worker-transport", string(cfg.Worker.TransportBackend), "worker transport backend")
	fs.StringVar((*string)(&cfg.Gateway.Runtime.StateBackend), "state-backend", string(cfg.Gateway.Runtime.StateBackend), "shared runtime state backend")
	fs.StringVar((*string)(&cfg.Gateway.Runtime.SnapshotBackend), "snapshot-backend", string(cfg.Gateway.Runtime.SnapshotBackend), "shared snapshot backend")
	fs.StringVar((*string)(&cfg.Gateway.Runtime.EventBackend), "event-backend", string(cfg.Gateway.Runtime.EventBackend), "shared event backend")
	fs.StringVar((*string)(&cfg.Gateway.Runtime.ThreadBackend), "thread-backend", string(cfg.Gateway.Runtime.ThreadBackend), "shared thread backend")
	fs.StringVar(&cfg.Gateway.Runtime.SnapshotStoreURL, "snapshot-store", cfg.Gateway.Runtime.SnapshotStoreURL, "shared snapshot store URL")
	fs.StringVar(&cfg.Gateway.Runtime.EventStoreURL, "event-store", cfg.Gateway.Runtime.EventStoreURL, "shared event store URL")
	fs.StringVar(&cfg.Gateway.Runtime.ThreadStoreURL, "thread-store", cfg.Gateway.Runtime.ThreadStoreURL, "shared thread store URL")
	stateBinding := statecmd.BindFlagsWithPrefix(fs, cfg.State, "state-service-", "state service ")
	fs.StringVar((*string)(&cfg.Worker.SandboxBackend), "sandbox-backend", string(cfg.Worker.SandboxBackend), "worker sandbox backend")
	fs.StringVar(&cfg.Worker.SandboxEndpoint, "sandbox-endpoint", cfg.Worker.SandboxEndpoint, "remote sandbox endpoint")
	fs.StringVar(&cfg.Worker.SandboxImage, "sandbox-image", cfg.Worker.SandboxImage, "container sandbox image")
	return &Binding{cfg: &cfg, stateBinding: stateBinding}
}

func (b *Binding) Config() Config {
	if b == nil || b.cfg == nil {
		return Config{}
	}
	cfg := *b.cfg
	if b.stateBinding != nil {
		cfg.State = b.stateBinding.Config()
	}
	return cfg.withDefaults()
}
