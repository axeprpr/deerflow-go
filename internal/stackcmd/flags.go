package stackcmd

import "flag"

type Binding struct {
	cfg *Config
}

func BindFlags(fs *flag.FlagSet, defaults Config) *Binding {
	cfg := defaults
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
	fs.StringVar((*string)(&cfg.Worker.SandboxBackend), "sandbox-backend", string(cfg.Worker.SandboxBackend), "worker sandbox backend")
	fs.StringVar(&cfg.Worker.SandboxEndpoint, "sandbox-endpoint", cfg.Worker.SandboxEndpoint, "remote sandbox endpoint")
	fs.StringVar(&cfg.Worker.SandboxImage, "sandbox-image", cfg.Worker.SandboxImage, "container sandbox image")
	return &Binding{cfg: &cfg}
}

func (b *Binding) Config() Config {
	if b == nil || b.cfg == nil {
		return Config{}
	}
	return b.cfg.withDefaults()
}
