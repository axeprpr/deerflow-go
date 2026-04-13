package runtimecmd

import "flag"

type NodeFlagBinding struct {
	defaults         NodeConfig
	role             *string
	addr             *string
	name             *string
	root             *string
	dataRoot         *string
	provider         *string
	endpoint         *string
	maxTurns         *int
	transportBackend *string
	sandboxBackend   *string
	sandboxEndpoint  *string
	sandboxImage     *string
	memoryStore      *string
	stateRoot        *string
	stateBackend     *string
	snapshotBackend  *string
	eventBackend     *string
	threadBackend    *string
}

func BindFlags(fs *flag.FlagSet, defaults NodeConfig, prefix, label string) *NodeFlagBinding {
	if fs == nil {
		fs = flag.CommandLine
	}
	return &NodeFlagBinding{
		defaults:         defaults,
		role:             fs.String(flagName(prefix, "role"), string(defaults.Role), label+"node role: worker|all-in-one|gateway"),
		addr:             fs.String(flagName(prefix, "addr"), defaults.Addr, label+"worker listen address"),
		name:             fs.String(flagName(prefix, "name"), defaults.Name, label+"node name"),
		root:             fs.String(flagName(prefix, "root"), defaults.Root, label+"node root"),
		dataRoot:         fs.String(flagName(prefix, "data-root"), defaults.DataRoot, label+"data root"),
		provider:         fs.String(flagName(prefix, "provider"), defaults.Provider, label+"LLM provider"),
		endpoint:         fs.String(flagName(prefix, "endpoint"), defaults.Endpoint, label+"worker endpoint for gateway role"),
		maxTurns:         fs.Int(flagName(prefix, "max-turns"), defaults.MaxTurns, label+"default max turns"),
		transportBackend: fs.String(flagName(prefix, "transport-backend"), string(defaults.TransportBackend), label+"transport backend: direct|queue|remote"),
		sandboxBackend:   fs.String(flagName(prefix, "sandbox-backend"), string(defaults.SandboxBackend), label+"sandbox backend: local-linux|container|remote|windows-restricted"),
		sandboxEndpoint:  fs.String(flagName(prefix, "sandbox-endpoint"), defaults.SandboxEndpoint, label+"sandbox endpoint for remote backend"),
		sandboxImage:     fs.String(flagName(prefix, "sandbox-image"), defaults.SandboxImage, label+"sandbox image for container backend"),
		memoryStore:      fs.String(flagName(prefix, "memory-store"), defaults.MemoryStoreURL, label+"shared memory store URL"),
		stateRoot:        fs.String(flagName(prefix, "state-root"), defaults.StateRoot, label+"state root"),
		stateBackend:     fs.String(flagName(prefix, "state-backend"), string(defaults.StateBackend), label+"state backend: in-memory|file|sqlite"),
		snapshotBackend:  fs.String(flagName(prefix, "snapshot-backend"), string(defaults.SnapshotBackend), label+"snapshot backend override: in-memory|file|sqlite"),
		eventBackend:     fs.String(flagName(prefix, "event-backend"), string(defaults.EventBackend), label+"event backend override: in-memory|file|sqlite"),
		threadBackend:    fs.String(flagName(prefix, "thread-backend"), string(defaults.ThreadBackend), label+"thread backend override: in-memory|file|sqlite"),
	}
}

func (b *NodeFlagBinding) Config() NodeConfig {
	if b == nil {
		return NodeConfig{}
	}
	defaults := b.defaults
	return NodeConfig{
		Role:             NormalizeRole(valueOrEmpty(b.role), defaults.Role),
		Addr:             NormalizeAddr(valueOrEmpty(b.addr), defaults.Addr),
		Name:             valueOrEmpty(b.name),
		Root:             valueOrEmpty(b.root),
		DataRoot:         valueOrEmpty(b.dataRoot),
		Provider:         valueOrEmpty(b.provider),
		Endpoint:         valueOrEmpty(b.endpoint),
		MaxTurns:         intValueOrDefault(b.maxTurns, defaults.MaxTurns),
		TransportBackend: NormalizeTransportBackend(valueOrEmpty(b.transportBackend), defaults.TransportBackend),
		SandboxBackend:   NormalizeSandboxBackend(valueOrEmpty(b.sandboxBackend), defaults.SandboxBackend),
		SandboxEndpoint:  valueOrEmpty(b.sandboxEndpoint),
		SandboxImage:     valueOrEmpty(b.sandboxImage),
		MemoryStoreURL:   valueOrEmpty(b.memoryStore),
		StateRoot:        valueOrEmpty(b.stateRoot),
		StateBackend:     NormalizeStateBackend(valueOrEmpty(b.stateBackend), defaults.StateBackend),
		SnapshotBackend:  NormalizeStateBackend(valueOrEmpty(b.snapshotBackend), defaults.SnapshotBackend),
		EventBackend:     NormalizeStateBackend(valueOrEmpty(b.eventBackend), defaults.EventBackend),
		ThreadBackend:    NormalizeStateBackend(valueOrEmpty(b.threadBackend), defaults.ThreadBackend),
	}
}

func flagName(prefix, name string) string {
	return prefix + name
}

func valueOrEmpty(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func intValueOrDefault(ptr *int, fallback int) int {
	if ptr == nil {
		return fallback
	}
	return *ptr
}
