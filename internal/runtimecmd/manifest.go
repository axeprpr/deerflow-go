package runtimecmd

import (
	"fmt"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
)

type NodeManifest struct {
	Role             harnessruntime.RuntimeNodeRole
	Preset           RuntimeNodePreset
	RuntimeProfile   string
	StateProfile     string
	ExecutionProfile string
	Addr             string
	Endpoint         string
	Transport        harnessruntime.WorkerTransportBackend
	Sandbox          harnessruntime.SandboxBackend
	MemoryStore      string
	StateProvider    harnessruntime.RuntimeStateProviderMode
	StateBackend     harnessruntime.RuntimeStateStoreBackend
	SnapshotBackend  harnessruntime.RuntimeStateStoreBackend
	EventBackend     harnessruntime.RuntimeStateStoreBackend
	ThreadBackend    harnessruntime.RuntimeStateStoreBackend
	StateRoot        string
	StateStore       string
	SnapshotStore    string
	EventStore       string
	ThreadStore      string
}

func (c NodeConfig) Manifest() NodeManifest {
	runtimeProfile := DefaultRuntimeProfile(c.effectivePreset(), c.Role)
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
