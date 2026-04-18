package sandboxcmd

import "flag"

type Binding struct {
	cfg *Config
}

func BindFlags(fs *flag.FlagSet, defaults Config) *Binding {
	return BindFlagsWithPrefix(fs, defaults, "", "")
}

func BindFlagsWithPrefix(fs *flag.FlagSet, defaults Config, prefix, label string) *Binding {
	cfg := defaults
	fs.StringVar(&cfg.Runtime.Addr, flagName(prefix, "addr"), cfg.Runtime.Addr, label+"runtime sandbox listen address")
	fs.StringVar(&cfg.Runtime.Name, flagName(prefix, "name"), cfg.Runtime.Name, label+"runtime sandbox service name")
	fs.StringVar(&cfg.Runtime.Root, flagName(prefix, "root"), cfg.Runtime.Root, label+"runtime sandbox root")
	fs.StringVar(&cfg.Runtime.DataRoot, flagName(prefix, "data-root"), cfg.Runtime.DataRoot, label+"runtime sandbox data root")
	fs.StringVar((*string)(&cfg.Runtime.Preset), flagName(prefix, "preset"), string(cfg.Runtime.Preset), label+"runtime sandbox preset: auto|fast-local|shared-sqlite|shared-remote")
	fs.StringVar((*string)(&cfg.Runtime.SandboxBackend), flagName(prefix, "backend"), string(cfg.Runtime.SandboxBackend), label+"sandbox backend: local-linux|container|remote|wsl2|windows-restricted")
	fs.StringVar(&cfg.Runtime.SandboxEndpoint, flagName(prefix, "endpoint"), cfg.Runtime.SandboxEndpoint, label+"remote sandbox endpoint")
	fs.StringVar(&cfg.Runtime.SandboxImage, flagName(prefix, "image"), cfg.Runtime.SandboxImage, label+"container sandbox image")
	fs.IntVar(&cfg.Runtime.SandboxMaxActiveLeases, flagName(prefix, "max-active-leases"), cfg.Runtime.SandboxMaxActiveLeases, label+"max active sandbox leases for remote sandbox server (<=0 means unlimited)")
	return &Binding{cfg: &cfg}
}

func (b *Binding) Config() Config {
	if b == nil || b.cfg == nil {
		return Config{}
	}
	cfg := *b.cfg
	return cfg.WithDefaults()
}

func flagName(prefix, name string) string {
	return prefix + name
}
