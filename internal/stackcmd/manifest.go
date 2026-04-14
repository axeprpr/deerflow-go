package stackcmd

import (
	"fmt"
	"strings"

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
	Processes      []ProcessManifest   `json:"processes"`
}

type ProcessManifest struct {
	Name      string        `json:"name"`
	Component ComponentKind `json:"component"`
	Addr      string        `json:"addr"`
	ReadyURL  string        `json:"ready_url"`
	DependsOn []string      `json:"depends_on,omitempty"`
	Binary    string        `json:"binary"`
	Args      []string      `json:"args"`
}

func (c Config) Manifest() StackManifest {
	cfg := c.withDefaults()
	spec := cfg.DeploymentSpec()
	launchSpec := cfg.LaunchSpec()
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
				ReadyURL:  launchSpec.GatewayHealthURL(),
				Dedicated: true,
			},
			{
				Kind:      ComponentWorker,
				Addr:      cfg.Worker.Addr,
				Node:      cfg.Worker.Manifest(),
				ReadyURL:  launchSpec.WorkerHealthURL(),
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
				ReadyURL:  launchSpec.WorkerStateHealthURL(),
				Dedicated: true,
			},
			ComponentManifest{
				Kind:      ComponentSandbox,
				Addr:      cfg.Sandbox.Runtime.Addr,
				Node:      cfg.Sandbox.Runtime.Manifest(),
				ReadyURL:  launchSpec.WorkerSandboxHealthURL(),
				Dedicated: true,
			},
		)
		manifest.Processes = []ProcessManifest{
			{
				Name:      "gateway",
				Component: ComponentGateway,
				Addr:      cfg.Gateway.Addr,
				ReadyURL:  launchSpec.GatewayHealthURL(),
				DependsOn: []string{"worker"},
				Binary:    "langgraph",
				Args:      cfg.Gateway.CLIArgs(),
			},
			{
				Name:      "worker",
				Component: ComponentWorker,
				Addr:      cfg.Worker.Addr,
				ReadyURL:  launchSpec.WorkerHealthURL(),
				DependsOn: []string{"state", "sandbox"},
				Binary:    "runtime-node",
				Args:      cfg.Worker.CLIArgs(""),
			},
			{
				Name:      "state",
				Component: ComponentState,
				Addr:      cfg.State.Runtime.Addr,
				ReadyURL:  launchSpec.WorkerStateHealthURL(),
				Binary:    "runtime-state",
				Args:      cfg.State.CLIArgs(),
			},
			{
				Name:      "sandbox",
				Component: ComponentSandbox,
				Addr:      cfg.Sandbox.Runtime.Addr,
				ReadyURL:  launchSpec.WorkerSandboxHealthURL(),
				Binary:    "runtime-sandbox",
				Args:      cfg.Sandbox.CLIArgs(),
			},
		}
		return manifest
	}
	manifest.Components = append(manifest.Components,
		ComponentManifest{
			Kind:      ComponentState,
			Addr:      cfg.Worker.Addr,
			Node:      cfg.Worker.Manifest(),
			ReadyURL:  launchSpec.WorkerStateHealthURL(),
			Dedicated: false,
		},
		ComponentManifest{
			Kind:      ComponentSandbox,
			Addr:      cfg.Worker.Addr,
			Node:      cfg.Worker.Manifest(),
			ReadyURL:  launchSpec.WorkerSandboxHealthURL(),
			Dedicated: false,
		},
	)
	manifest.Processes = []ProcessManifest{
		{
			Name:      "gateway",
			Component: ComponentGateway,
			Addr:      cfg.Gateway.Addr,
			ReadyURL:  launchSpec.GatewayHealthURL(),
			DependsOn: []string{"worker"},
			Binary:    "langgraph",
			Args:      cfg.Gateway.CLIArgs(),
		},
		{
			Name:      "worker",
			Component: ComponentWorker,
			Addr:      cfg.Worker.Addr,
			ReadyURL:  launchSpec.WorkerHealthURL(),
			Binary:    "runtime-node",
			Args:      cfg.Worker.CLIArgs(""),
		},
	}
	return manifest
}

func (m StackManifest) ValidateProcessGraph() error {
	if len(m.Processes) == 0 {
		return nil
	}
	processByName := make(map[string]ProcessManifest, len(m.Processes))
	for _, process := range m.Processes {
		name := strings.TrimSpace(process.Name)
		if name == "" {
			return fmt.Errorf("process name is required")
		}
		if _, exists := processByName[name]; exists {
			return fmt.Errorf("duplicate process %q", name)
		}
		process.Name = name
		processByName[name] = process
	}
	for _, process := range processByName {
		for _, dep := range process.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				return fmt.Errorf("process %q has empty dependency", process.Name)
			}
			if dep == process.Name {
				return fmt.Errorf("process %q cannot depend on itself", process.Name)
			}
			if _, ok := processByName[dep]; !ok {
				return fmt.Errorf("process %q depends on unknown process %q", process.Name, dep)
			}
		}
	}
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("cycle detected at %q", name)
		case 2:
			return nil
		}
		state[name] = 1
		process := processByName[name]
		for _, dep := range process.DependsOn {
			if err := visit(dep); err != nil {
				return fmt.Errorf("%s -> %w", name, err)
			}
		}
		state[name] = 2
		return nil
	}
	for name := range processByName {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
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
