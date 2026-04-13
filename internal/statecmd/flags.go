package statecmd

import "flag"

type Binding struct {
	cfg *Config
}

func BindFlags(fs *flag.FlagSet, defaults Config) *Binding {
	return BindFlagsWithPrefix(fs, defaults, "", "")
}

func BindFlagsWithPrefix(fs *flag.FlagSet, defaults Config, prefix, label string) *Binding {
	cfg := defaults
	fs.StringVar(&cfg.Runtime.Addr, flagName(prefix, "addr"), cfg.Runtime.Addr, label+"runtime state listen address")
	fs.StringVar(&cfg.Runtime.Name, flagName(prefix, "name"), cfg.Runtime.Name, label+"runtime state service name")
	fs.StringVar(&cfg.Runtime.Root, flagName(prefix, "root"), cfg.Runtime.Root, label+"runtime state root")
	fs.StringVar(&cfg.Runtime.DataRoot, flagName(prefix, "data-root"), cfg.Runtime.DataRoot, label+"runtime state data root")
	fs.StringVar((*string)(&cfg.Runtime.Preset), flagName(prefix, "preset"), string(cfg.Runtime.Preset), label+"runtime state preset: auto|fast-local|shared-sqlite|shared-remote")
	fs.StringVar((*string)(&cfg.Runtime.StateProvider), flagName(prefix, "state-provider"), string(cfg.Runtime.StateProvider), label+"state provider: auto|isolated|shared-sqlite")
	fs.StringVar(&cfg.Runtime.StateRoot, flagName(prefix, "state-root"), cfg.Runtime.StateRoot, label+"runtime state root")
	fs.StringVar((*string)(&cfg.Runtime.StateBackend), flagName(prefix, "state-backend"), string(cfg.Runtime.StateBackend), label+"runtime state backend: in-memory|file|sqlite|remote")
	fs.StringVar(&cfg.Runtime.StateStoreURL, flagName(prefix, "state-store"), cfg.Runtime.StateStoreURL, label+"runtime shared state store URL/path")
	fs.StringVar((*string)(&cfg.Runtime.SnapshotBackend), flagName(prefix, "snapshot-backend"), string(cfg.Runtime.SnapshotBackend), label+"snapshot backend override: in-memory|file|sqlite|remote")
	fs.StringVar((*string)(&cfg.Runtime.EventBackend), flagName(prefix, "event-backend"), string(cfg.Runtime.EventBackend), label+"event backend override: in-memory|file|sqlite|remote")
	fs.StringVar((*string)(&cfg.Runtime.ThreadBackend), flagName(prefix, "thread-backend"), string(cfg.Runtime.ThreadBackend), label+"thread backend override: in-memory|file|sqlite|remote")
	fs.StringVar(&cfg.Runtime.SnapshotStoreURL, flagName(prefix, "snapshot-store"), cfg.Runtime.SnapshotStoreURL, label+"snapshot store URL/path")
	fs.StringVar(&cfg.Runtime.EventStoreURL, flagName(prefix, "event-store"), cfg.Runtime.EventStoreURL, label+"event store URL/path")
	fs.StringVar(&cfg.Runtime.ThreadStoreURL, flagName(prefix, "thread-store"), cfg.Runtime.ThreadStoreURL, label+"thread store URL/path")
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
