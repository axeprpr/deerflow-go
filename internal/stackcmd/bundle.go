package stackcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type HostPlan struct {
	Version   string            `json:"version"`
	Processes []HostProcessPlan `json:"processes"`
	Systemd   HostSystemdPlan   `json:"systemd"`
	Electron  HostElectronPlan  `json:"electron"`
}

type HostProcessPlan struct {
	Name              string   `json:"name"`
	Binary            string   `json:"binary"`
	Args              []string `json:"args"`
	DependsOn         []string `json:"depends_on,omitempty"`
	ReadyURL          string   `json:"ready_url"`
	RestartPolicy     string   `json:"restart_policy"`
	MaxRestarts       int      `json:"max_restarts"`
	RestartDelayMilli int64    `json:"restart_delay_ms"`
}

type HostSystemdPlan struct {
	UnitPrefix string               `json:"unit_prefix"`
	Services   []HostSystemdService `json:"services"`
}

type HostSystemdService struct {
	Name              string   `json:"name"`
	After             []string `json:"after,omitempty"`
	Wants             []string `json:"wants,omitempty"`
	ExecStart         []string `json:"exec_start"`
	Restart           string   `json:"restart"`
	RestartDelayMilli int64    `json:"restart_delay_ms"`
}

type HostElectronPlan struct {
	StartOrder    []string `json:"start_order"`
	ShutdownOrder []string `json:"shutdown_order"`
}

const bundleDefaultRestartPolicy = ProcessRestartOnFailure
const bundleDefaultMaxRestarts = 3
const bundleDefaultRestartDelay = 500 * time.Millisecond

func WriteBundle(dir string, manifest StackManifest) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("bundle dir required")
	}
	if err := manifest.ValidateProcessGraph(); err != nil {
		return fmt.Errorf("invalid process graph: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := commandrun.WriteJSONFile(filepath.Join(dir, "stack-manifest.json"), manifest); err != nil {
		return err
	}
	processDir := filepath.Join(dir, "processes")
	if err := os.MkdirAll(processDir, 0o755); err != nil {
		return err
	}
	for _, process := range manifest.Processes {
		if err := commandrun.WriteJSONFile(filepath.Join(processDir, process.Name+".json"), process); err != nil {
			return err
		}
	}
	hostPlan, err := buildHostPlan(manifest)
	if err != nil {
		return err
	}
	if err := commandrun.WriteJSONFile(filepath.Join(dir, "host-plan.json"), hostPlan); err != nil {
		return err
	}
	return nil
}

func buildHostPlan(manifest StackManifest) (HostPlan, error) {
	order, err := resolveProcessOrder(manifest.Processes)
	if err != nil {
		return HostPlan{}, err
	}
	processByName := make(map[string]ProcessManifest, len(manifest.Processes))
	for _, process := range manifest.Processes {
		processByName[strings.TrimSpace(process.Name)] = process
	}
	processes := make([]HostProcessPlan, 0, len(order))
	services := make([]HostSystemdService, 0, len(order))
	startOrder := make([]string, 0, len(order))
	for _, name := range order {
		process, ok := processByName[name]
		if !ok {
			continue
		}
		deps := append([]string(nil), process.DependsOn...)
		plan := HostProcessPlan{
			Name:              process.Name,
			Binary:            process.Binary,
			Args:              append([]string(nil), process.Args...),
			DependsOn:         deps,
			ReadyURL:          process.ReadyURL,
			RestartPolicy:     string(bundleDefaultRestartPolicy),
			MaxRestarts:       bundleDefaultMaxRestarts,
			RestartDelayMilli: bundleDefaultRestartDelay.Milliseconds(),
		}
		processes = append(processes, plan)
		startOrder = append(startOrder, process.Name)

		afterUnits := make([]string, 0, len(deps))
		for _, dep := range deps {
			if trimmed := strings.TrimSpace(dep); trimmed != "" {
				afterUnits = append(afterUnits, systemdServiceName(trimmed))
			}
		}
		services = append(services, HostSystemdService{
			Name:              systemdServiceName(process.Name),
			After:             append([]string(nil), afterUnits...),
			Wants:             append([]string(nil), afterUnits...),
			ExecStart:         append([]string{process.Binary}, process.Args...),
			Restart:           systemdRestartMode(bundleDefaultRestartPolicy),
			RestartDelayMilli: bundleDefaultRestartDelay.Milliseconds(),
		})
	}
	shutdownOrder := make([]string, 0, len(startOrder))
	for i := len(startOrder) - 1; i >= 0; i-- {
		shutdownOrder = append(shutdownOrder, startOrder[i])
	}
	return HostPlan{
		Version:   "v1",
		Processes: processes,
		Systemd: HostSystemdPlan{
			UnitPrefix: "deerflow-runtime-",
			Services:   services,
		},
		Electron: HostElectronPlan{
			StartOrder:    startOrder,
			ShutdownOrder: shutdownOrder,
		},
	}, nil
}

func systemdServiceName(processName string) string {
	return "deerflow-runtime-" + strings.TrimSpace(processName) + ".service"
}

func systemdRestartMode(policy ProcessRestartPolicy) string {
	switch policy {
	case ProcessRestartNever:
		return "no"
	case ProcessRestartAlways:
		return "always"
	default:
		return "on-failure"
	}
}
