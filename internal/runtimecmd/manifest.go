package runtimecmd

import (
	"fmt"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type NodeManifest struct {
	Role             harnessruntime.RuntimeNodeRole          `json:"role"`
	Preset           RuntimeNodePreset                       `json:"preset"`
	RuntimeProfile   string                                  `json:"runtime_profile"`
	StateProfile     string                                  `json:"state_profile"`
	ExecutionProfile string                                  `json:"execution_profile"`
	Addr             string                                  `json:"addr"`
	Endpoint         string                                  `json:"endpoint"`
	Transport        harnessruntime.WorkerTransportBackend   `json:"transport"`
	Sandbox          harnessruntime.SandboxBackend           `json:"sandbox"`
	SandboxProfile   harnessruntime.SandboxProfile           `json:"sandbox_profile"`
	MemoryStore      string                                  `json:"memory_store"`
	StateProvider    harnessruntime.RuntimeStateProviderMode `json:"state_provider"`
	StateBackend     harnessruntime.RuntimeStateStoreBackend `json:"state_backend"`
	SnapshotBackend  harnessruntime.RuntimeStateStoreBackend `json:"snapshot_backend"`
	EventBackend     harnessruntime.RuntimeStateStoreBackend `json:"event_backend"`
	ThreadBackend    harnessruntime.RuntimeStateStoreBackend `json:"thread_backend"`
	StateRoot        string                                  `json:"state_root"`
	StateStore       string                                  `json:"state_store"`
	SnapshotStore    string                                  `json:"snapshot_store"`
	EventStore       string                                  `json:"event_store"`
	ThreadStore      string                                  `json:"thread_store"`
}

func (c NodeConfig) Manifest() NodeManifest {
	runtimeProfile := DefaultRuntimeProfile(c.effectivePreset(), c.Role)
	sandboxProfile := harnessruntime.DescribeSandboxProfile(c.RuntimeNodeConfig().Sandbox)
	return NodeManifest{
		Role:             c.Role,
		Preset:           c.effectivePreset(),
		RuntimeProfile:   runtimeProfile.Name,
		StateProfile:     runtimeProfile.State.Name,
		ExecutionProfile: runtimeProfile.Execution.Name,
		Addr:             c.Addr,
		Endpoint:         c.Endpoint,
		Transport:        c.TransportBackend,
		Sandbox:          c.SandboxBackend,
		SandboxProfile:   sandboxProfile,
		MemoryStore:      c.MemoryStoreURL,
		StateProvider:    c.StateProvider,
		StateBackend:     c.StateBackend,
		SnapshotBackend:  c.SnapshotBackend,
		EventBackend:     c.EventBackend,
		ThreadBackend:    c.ThreadBackend,
		StateRoot:        c.StateRoot,
		StateStore:       c.StateStoreURL,
		SnapshotStore:    c.SnapshotStoreURL,
		EventStore:       c.EventStoreURL,
		ThreadStore:      c.ThreadStoreURL,
	}
}

func (m NodeManifest) StartupLines() []string {
	return []string{
		fmt.Sprintf("runtime node starting role=%s preset=%s profile=%s", m.Role, m.Preset, firstNonEmpty(m.RuntimeProfile, "(default)")),
		fmt.Sprintf("  transport=%s endpoint=%s execution_profile=%s", m.Transport, firstNonEmpty(m.Endpoint, "(local)"), firstNonEmpty(m.ExecutionProfile, "(default)")),
		fmt.Sprintf("  worker_addr=%s", m.Addr),
		fmt.Sprintf("  sandbox=%s", m.Sandbox),
		fmt.Sprintf("  sandbox_profile=isolation=%s capabilities=%s heartbeat_ms=%d idle_ttl_ms=%d", firstNonEmpty(m.SandboxProfile.Isolation, "(unknown)"), strings.Join(m.SandboxProfile.Capabilities, ","), m.SandboxProfile.Limits.HeartbeatIntervalMilli, m.SandboxProfile.Limits.IdleTTLMilli),
		fmt.Sprintf("  memory_store=%s", firstNonEmpty(m.MemoryStore, "(file-store)")),
		fmt.Sprintf("  state_profile=%s state_provider=%s state=%s snapshot=%s event=%s thread=%s root=%s store=%s", firstNonEmpty(m.StateProfile, "(default)"), firstNonEmpty(string(m.StateProvider), "(auto)"), firstNonEmpty(string(m.StateBackend), "(default)"), firstNonEmpty(string(m.SnapshotBackend), "(default)"), firstNonEmpty(string(m.EventBackend), "(default)"), firstNonEmpty(string(m.ThreadBackend), "(default)"), firstNonEmpty(m.StateRoot, "(memory)"), firstNonEmpty(m.StateStore, "(derived)")),
		fmt.Sprintf("  snapshot_store=%s event_store=%s thread_store=%s", firstNonEmpty(m.SnapshotStore, "(derived)"), firstNonEmpty(m.EventStore, "(derived)"), firstNonEmpty(m.ThreadStore, "(derived)")),
	}
}

func (m NodeManifest) ReadyLine(spec harnessruntime.RuntimeNodeLaunchSpec) (string, error) {
	if !spec.ServesRemoteWorker {
		return "", fmt.Errorf("runtime node role %q does not expose a remote worker server", spec.Role)
	}
	return fmt.Sprintf("runtime node ready role=%s addr=%s sandbox=%t state=%t profile=%s", spec.Role, spec.RemoteWorkerAddr, spec.ServesRemoteSandbox, spec.ServesRemoteState, firstNonEmpty(m.RuntimeProfile, "(default)")), nil
}
