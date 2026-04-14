package stackcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type HostPlan struct {
	Version       string            `json:"version"`
	RuntimePolicy HostRuntimePolicy `json:"runtime_policy"`
	Processes     []HostProcessPlan `json:"processes"`
	Systemd       HostSystemdPlan   `json:"systemd"`
	Electron      HostElectronPlan  `json:"electron"`
}

type HostRuntimePolicy struct {
	RestartPolicy          string `json:"restart_policy"`
	MaxRestarts            int    `json:"max_restarts"`
	RestartDelayMilli      int64  `json:"restart_delay_ms"`
	DependencyTimeoutMilli int64  `json:"dependency_timeout_ms"`
	FailureIsolation       bool   `json:"failure_isolation"`
}

type HostProcessPlan struct {
	Name                   string   `json:"name"`
	Binary                 string   `json:"binary"`
	Args                   []string `json:"args"`
	DependsOn              []string `json:"depends_on,omitempty"`
	ReadyURL               string   `json:"ready_url"`
	RestartPolicy          string   `json:"restart_policy"`
	MaxRestarts            int      `json:"max_restarts"`
	RestartDelayMilli      int64    `json:"restart_delay_ms"`
	DependencyTimeoutMilli int64    `json:"dependency_timeout_ms"`
	FailureIsolation       bool     `json:"failure_isolation"`
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
const bundleDefaultDependencyTimeout = 60 * time.Second
const bundleDefaultFailureIsolation = false

type BundleOptions struct {
	RestartPolicy     ProcessRestartPolicy
	MaxRestarts       int
	RestartDelay      time.Duration
	DependencyTimeout time.Duration
	FailureIsolation  bool
}

func defaultBundleOptions() BundleOptions {
	return BundleOptions{
		RestartPolicy:     bundleDefaultRestartPolicy,
		MaxRestarts:       bundleDefaultMaxRestarts,
		RestartDelay:      bundleDefaultRestartDelay,
		DependencyTimeout: bundleDefaultDependencyTimeout,
		FailureIsolation:  bundleDefaultFailureIsolation,
	}
}

func normalizeBundleOptions(options BundleOptions) (BundleOptions, error) {
	normalized := defaultBundleOptions()
	if parsed, err := parseProcessRestartPolicy(string(options.RestartPolicy)); err != nil {
		return BundleOptions{}, err
	} else {
		normalized.RestartPolicy = parsed
	}
	normalized.MaxRestarts = options.MaxRestarts
	if options.RestartDelay >= 0 {
		normalized.RestartDelay = options.RestartDelay
	}
	if options.DependencyTimeout > 0 {
		normalized.DependencyTimeout = options.DependencyTimeout
	}
	normalized.FailureIsolation = options.FailureIsolation
	return normalized, nil
}

func WriteBundle(dir string, manifest StackManifest) error {
	return WriteBundleWithOptions(dir, manifest, BundleOptions{})
}

func WriteBundleWithOptions(dir string, manifest StackManifest, options BundleOptions) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("bundle dir required")
	}
	if err := manifest.ValidateProcessGraph(); err != nil {
		return fmt.Errorf("invalid process graph: %w", err)
	}
	normalizedOptions, err := normalizeBundleOptions(options)
	if err != nil {
		return err
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
	hostPlan, err := buildHostPlan(manifest, normalizedOptions)
	if err != nil {
		return err
	}
	if err := commandrun.WriteJSONFile(filepath.Join(dir, "host-plan.json"), hostPlan); err != nil {
		return err
	}
	if err := writeHostAssets(dir, hostPlan); err != nil {
		return err
	}
	return nil
}

func buildHostPlan(manifest StackManifest, options BundleOptions) (HostPlan, error) {
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
			Name:                   process.Name,
			Binary:                 process.Binary,
			Args:                   append([]string(nil), process.Args...),
			DependsOn:              deps,
			ReadyURL:               process.ReadyURL,
			RestartPolicy:          string(options.RestartPolicy),
			MaxRestarts:            options.MaxRestarts,
			RestartDelayMilli:      options.RestartDelay.Milliseconds(),
			DependencyTimeoutMilli: options.DependencyTimeout.Milliseconds(),
			FailureIsolation:       options.FailureIsolation,
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
			Restart:           systemdRestartMode(options.RestartPolicy),
			RestartDelayMilli: options.RestartDelay.Milliseconds(),
		})
	}
	shutdownOrder := make([]string, 0, len(startOrder))
	for i := len(startOrder) - 1; i >= 0; i-- {
		shutdownOrder = append(shutdownOrder, startOrder[i])
	}
	return HostPlan{
		Version: "v1",
		RuntimePolicy: HostRuntimePolicy{
			RestartPolicy:          string(options.RestartPolicy),
			MaxRestarts:            options.MaxRestarts,
			RestartDelayMilli:      options.RestartDelay.Milliseconds(),
			DependencyTimeoutMilli: options.DependencyTimeout.Milliseconds(),
			FailureIsolation:       options.FailureIsolation,
		},
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

type ElectronHostBundle struct {
	Version       string                `json:"version"`
	StartOrder    []string              `json:"start_order"`
	ShutdownOrder []string              `json:"shutdown_order"`
	Processes     []ElectronHostProcess `json:"processes"`
}

type ElectronHostProcess struct {
	Name                   string   `json:"name"`
	Command                []string `json:"command"`
	DependsOn              []string `json:"depends_on,omitempty"`
	ReadyURL               string   `json:"ready_url"`
	RestartPolicy          string   `json:"restart_policy"`
	MaxRestarts            int      `json:"max_restarts"`
	RestartDelayMilli      int64    `json:"restart_delay_ms"`
	DependencyTimeoutMilli int64    `json:"dependency_timeout_ms"`
	FailureIsolation       bool     `json:"failure_isolation"`
}

func writeHostAssets(dir string, hostPlan HostPlan) error {
	hostDir := filepath.Join(dir, "host")
	systemdDir := filepath.Join(hostDir, "systemd")
	electronDir := filepath.Join(hostDir, "electron")
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(electronDir, 0o755); err != nil {
		return err
	}

	for _, service := range hostPlan.Systemd.Services {
		if err := os.WriteFile(filepath.Join(systemdDir, service.Name), []byte(renderSystemdUnit(service)), 0o644); err != nil {
			return err
		}
	}

	byProcess := make(map[string]HostProcessPlan, len(hostPlan.Processes))
	for _, process := range hostPlan.Processes {
		byProcess[strings.TrimSpace(process.Name)] = process
	}
	electronProcesses := make([]ElectronHostProcess, 0, len(hostPlan.Electron.StartOrder))
	for _, name := range hostPlan.Electron.StartOrder {
		process, ok := byProcess[strings.TrimSpace(name)]
		if !ok {
			continue
		}
		electronProcesses = append(electronProcesses, ElectronHostProcess{
			Name:                   process.Name,
			Command:                append([]string{process.Binary}, process.Args...),
			DependsOn:              append([]string(nil), process.DependsOn...),
			ReadyURL:               process.ReadyURL,
			RestartPolicy:          process.RestartPolicy,
			MaxRestarts:            process.MaxRestarts,
			RestartDelayMilli:      process.RestartDelayMilli,
			DependencyTimeoutMilli: process.DependencyTimeoutMilli,
			FailureIsolation:       process.FailureIsolation,
		})
	}
	electronBundle := ElectronHostBundle{
		Version:       hostPlan.Version,
		StartOrder:    append([]string(nil), hostPlan.Electron.StartOrder...),
		ShutdownOrder: append([]string(nil), hostPlan.Electron.ShutdownOrder...),
		Processes:     electronProcesses,
	}
	if err := commandrun.WriteJSONFile(filepath.Join(electronDir, "runtime-processes.json"), electronBundle); err != nil {
		return err
	}
	return nil
}

func renderSystemdUnit(service HostSystemdService) string {
	lines := []string{
		"[Unit]",
		"Description=DeerFlow Runtime Process " + strings.TrimSpace(service.Name),
		"After=network-online.target",
		"Wants=network-online.target",
	}
	if len(service.After) > 0 {
		lines = append(lines, "After=network-online.target "+strings.Join(service.After, " "))
	}
	if len(service.Wants) > 0 {
		lines = append(lines, "Wants=network-online.target "+strings.Join(service.Wants, " "))
	}
	lines = append(lines,
		"",
		"[Service]",
		"Type=simple",
		"ExecStart="+renderSystemdExecStart(service.ExecStart),
		"Restart="+firstNonEmpty(strings.TrimSpace(service.Restart), "on-failure"),
		"RestartSec="+formatSystemdDurationMS(service.RestartDelayMilli),
		"KillSignal=SIGINT",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
	)
	return strings.Join(lines, "\n") + "\n"
}

func renderSystemdExecStart(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		quoted = append(quoted, strconv.Quote(value))
	}
	return strings.Join(quoted, " ")
}

func formatSystemdDurationMS(ms int64) string {
	if ms <= 0 {
		return "0"
	}
	return strconv.FormatInt(ms, 10) + "ms"
}
