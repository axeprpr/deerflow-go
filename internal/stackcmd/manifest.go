package stackcmd

import (
	"fmt"

	"github.com/axeprpr/deerflow-go/internal/langgraphcmd"
	"github.com/axeprpr/deerflow-go/internal/runtimecmd"
)

type ComponentManifest struct {
	Kind      ComponentKind           `json:"kind"`
	Addr      string                  `json:"addr"`
	Node      runtimecmd.NodeManifest `json:"node"`
	ReadyURL  string                  `json:"ready_url"`
	Dedicated bool                    `json:"dedicated"`
}

type StackManifest struct {
	Preset         StackPreset         `json:"preset"`
	Profile        StackProfile        `json:"profile"`
	GatewayAddr    string              `json:"gateway_addr"`
	WorkerRoot     string              `json:"worker_root"`
	WorkerStore    string              `json:"worker_store"`
	WorkerSnapshot string              `json:"worker_snapshot"`
	WorkerEvent    string              `json:"worker_event"`
	WorkerThread   string              `json:"worker_thread"`
	WorkerDispatch string              `json:"worker_dispatch"`
	Components     []ComponentManifest `json:"components"`
}

func (c Config) Manifest() StackManifest {
	cfg := c.withDefaults()
	spec := cfg.DeploymentSpec()
	manifest := StackManifest{
		Preset:         cfg.effectivePreset(),
		Profile:        cfg.Profile(),
		GatewayAddr:    cfg.Gateway.Addr,
		WorkerRoot:     firstNonEmpty(cfg.Worker.StateRoot, "(memory)"),
		WorkerStore:    firstNonEmpty(cfg.Worker.StateStoreURL, "(derived)"),
		WorkerSnapshot: firstNonEmpty(cfg.Worker.SnapshotStoreURL, "(derived)"),
		WorkerEvent:    firstNonEmpty(cfg.Worker.EventStoreURL, "(derived)"),
		WorkerThread:   firstNonEmpty(cfg.Worker.ThreadStoreURL, "(derived)"),
		WorkerDispatch: spec.WorkerDispatch,
		Components: []ComponentManifest{
			{
				Kind:      ComponentGateway,
				Addr:      cfg.Gateway.Addr,
				Node:      cfg.Gateway.Runtime.Manifest(),
				ReadyURL:  cfg.LaunchSpec().GatewayHealthURL(),
				Dedicated: true,
			},
			{
				Kind:      ComponentWorker,
				Addr:      cfg.Worker.Addr,
				Node:      cfg.Worker.Manifest(),
				ReadyURL:  cfg.LaunchSpec().WorkerHealthURL(),
				Dedicated: true,
			},
		},
	}
	if cfg.usesDedicatedStateService() {
		manifest.Components = append(manifest.Components,
			ComponentManifest{
				Kind:      ComponentState,
				Addr:      cfg.State.Runtime.Addr,
				Node:      cfg.State.Runtime.Manifest(),
				ReadyURL:  cfg.LaunchSpec().WorkerStateHealthURL(),
				Dedicated: true,
			},
			ComponentManifest{
				Kind:      ComponentSandbox,
				Addr:      cfg.Sandbox.Runtime.Addr,
				Node:      cfg.Sandbox.Runtime.Manifest(),
				ReadyURL:  cfg.LaunchSpec().WorkerSandboxHealthURL(),
				Dedicated: true,
			},
		)
		return manifest
	}
	manifest.Components = append(manifest.Components,
		ComponentManifest{
			Kind:      ComponentState,
			Addr:      cfg.Worker.Addr,
			Node:      cfg.Worker.Manifest(),
			ReadyURL:  cfg.LaunchSpec().WorkerStateHealthURL(),
			Dedicated: false,
		},
		ComponentManifest{
			Kind:      ComponentSandbox,
			Addr:      cfg.Worker.Addr,
			Node:      cfg.Worker.Manifest(),
			ReadyURL:  cfg.LaunchSpec().WorkerSandboxHealthURL(),
			Dedicated: false,
		},
	)
	return manifest
}

func (m StackManifest) StartupLines(build langgraphcmd.BuildInfo, yolo bool, logLevel string) []string {
	lines := []string{
		fmt.Sprintf("Starting split runtime stack preset=%s...", m.Preset),
		fmt.Sprintf("  gateway=%s worker_dispatch=%s", m.GatewayAddr, m.WorkerDispatch),
	}
	for _, component := range m.Components {
		switch component.Kind {
		case ComponentGateway:
			continue
		case ComponentWorker:
			lines = append(lines,
				fmt.Sprintf("  Worker: role=%s addr=%s transport=%s sandbox=%s state_provider=%s", component.Node.Role, component.Addr, component.Node.Transport, component.Node.Sandbox, component.Node.StateProvider),
				fmt.Sprintf("  Shared state: root=%s store=%s snapshot=%s event=%s thread=%s", m.WorkerRoot, m.WorkerStore, m.WorkerSnapshot, m.WorkerEvent, m.WorkerThread),
				fmt.Sprintf("  Worker manifest: preset=%s profile=%s", component.Node.Preset, firstNonEmpty(component.Node.RuntimeProfile, "(default)")),
			)
		case ComponentState:
			lines = append(lines, fmt.Sprintf("  %s: addr=%s provider=%s store=%s", component.KindLabel(), component.Addr, firstNonEmpty(string(component.Node.StateProvider), "(auto)"), firstNonEmpty(component.Node.StateStore, "(derived)")))
		case ComponentSandbox:
			lines = append(lines, fmt.Sprintf("  %s: addr=%s backend=%s", component.KindLabel(), component.Addr, firstNonEmpty(string(component.Node.Sandbox), "(default)")))
		}
	}
	return lines
}

func (m StackManifest) ReadyLines() []string {
	lines := make([]string, 0, len(m.Components))
	for _, component := range m.Components {
		switch component.Kind {
		case ComponentWorker:
			lines = append(lines, fmt.Sprintf("  Worker server: %s", m.WorkerDispatch))
		case ComponentState:
			lines = append(lines, fmt.Sprintf("  %s: %s", component.KindLabel(), component.ReadyURL))
		case ComponentSandbox:
			lines = append(lines, fmt.Sprintf("  %s: %s", component.KindLabel(), component.ReadyURL))
		}
	}
	return lines
}

func (m StackManifest) ReadyTargets() []string {
	targets := make([]string, 0, len(m.Components))
	for _, component := range m.Components {
		if component.Kind == ComponentGateway {
			continue
		}
		if component.ReadyURL != "" {
			targets = append(targets, component.ReadyURL)
		}
	}
	return targets
}

func (c ComponentManifest) KindLabel() string {
	switch c.Kind {
	case ComponentState:
		if c.Dedicated {
			return "State server"
		}
		return "Worker state"
	case ComponentSandbox:
		if c.Dedicated {
			return "Sandbox server"
		}
		return "Worker sandbox"
	default:
		return string(c.Kind)
	}
}
