package stackcmd

import (
	"encoding/json"
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

func ValidateBundle(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("bundle dir required")
	}
	manifest, err := LoadBundleManifest(dir)
	if err != nil {
		return err
	}

	var hostPlan HostPlan
	if err := readJSONFile(filepath.Join(dir, "host-plan.json"), &hostPlan); err != nil {
		return fmt.Errorf("read host-plan.json: %w", err)
	}
	if err := validateHostPlan(manifest, hostPlan); err != nil {
		return fmt.Errorf("invalid host plan: %w", err)
	}

	var electronBundle ElectronHostBundle
	if err := readJSONFile(filepath.Join(dir, "host", "electron", "runtime-processes.json"), &electronBundle); err != nil {
		return fmt.Errorf("read host/electron/runtime-processes.json: %w", err)
	}
	if err := validateElectronBundle(hostPlan, electronBundle); err != nil {
		return fmt.Errorf("invalid electron host bundle: %w", err)
	}
	for _, service := range hostPlan.Systemd.Services {
		if _, err := os.Stat(filepath.Join(dir, "host", "systemd", service.Name)); err != nil {
			return fmt.Errorf("missing host/systemd/%s: %w", service.Name, err)
		}
	}
	return nil
}

func LoadBundleManifest(dir string) (StackManifest, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return StackManifest{}, fmt.Errorf("bundle dir required")
	}
	var manifest StackManifest
	if err := readJSONFile(filepath.Join(dir, "stack-manifest.json"), &manifest); err != nil {
		return StackManifest{}, fmt.Errorf("read stack-manifest.json: %w", err)
	}
	if err := manifest.ValidateProcessGraph(); err != nil {
		return StackManifest{}, fmt.Errorf("invalid process graph: %w", err)
	}
	return manifest, nil
}

func LoadBundleHostPlan(dir string) (HostPlan, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return HostPlan{}, fmt.Errorf("bundle dir required")
	}
	manifest, err := LoadBundleManifest(dir)
	if err != nil {
		return HostPlan{}, err
	}
	var hostPlan HostPlan
	if err := readJSONFile(filepath.Join(dir, "host-plan.json"), &hostPlan); err != nil {
		return HostPlan{}, fmt.Errorf("read host-plan.json: %w", err)
	}
	if err := validateHostPlan(manifest, hostPlan); err != nil {
		return HostPlan{}, fmt.Errorf("invalid host plan: %w", err)
	}
	return hostPlan, nil
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

func readJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func validateHostPlan(manifest StackManifest, hostPlan HostPlan) error {
	manifestByName := make(map[string]ProcessManifest, len(manifest.Processes))
	for _, process := range manifest.Processes {
		manifestByName[strings.TrimSpace(process.Name)] = process
	}
	if len(hostPlan.Processes) != len(manifestByName) {
		return fmt.Errorf("process count mismatch: host=%d manifest=%d", len(hostPlan.Processes), len(manifestByName))
	}
	if _, err := parseProcessRestartPolicy(hostPlan.RuntimePolicy.RestartPolicy); err != nil {
		return err
	}

	hostByName := make(map[string]HostProcessPlan, len(hostPlan.Processes))
	for _, process := range hostPlan.Processes {
		name := strings.TrimSpace(process.Name)
		if name == "" {
			return fmt.Errorf("host process name is required")
		}
		if _, ok := manifestByName[name]; !ok {
			return fmt.Errorf("host process %q missing from manifest", name)
		}
		if _, exists := hostByName[name]; exists {
			return fmt.Errorf("duplicate host process %q", name)
		}
		if _, err := parseProcessRestartPolicy(process.RestartPolicy); err != nil {
			return err
		}
		hostByName[name] = process
	}
	for name := range manifestByName {
		if _, ok := hostByName[name]; !ok {
			return fmt.Errorf("host plan missing process %q", name)
		}
	}
	if err := validateOrderSet("start_order", hostPlan.Electron.StartOrder, manifestByName); err != nil {
		return err
	}
	if err := validateOrderSet("shutdown_order", hostPlan.Electron.ShutdownOrder, manifestByName); err != nil {
		return err
	}
	if len(hostPlan.Systemd.Services) != len(manifestByName) {
		return fmt.Errorf("systemd service count mismatch: services=%d manifest=%d", len(hostPlan.Systemd.Services), len(manifestByName))
	}
	return nil
}

func validateElectronBundle(hostPlan HostPlan, bundle ElectronHostBundle) error {
	if len(bundle.Processes) != len(hostPlan.Electron.StartOrder) {
		return fmt.Errorf("process count mismatch: electron=%d host=%d", len(bundle.Processes), len(hostPlan.Electron.StartOrder))
	}
	if len(bundle.StartOrder) != len(hostPlan.Electron.StartOrder) || len(bundle.ShutdownOrder) != len(hostPlan.Electron.ShutdownOrder) {
		return fmt.Errorf("electron order size mismatch")
	}
	for i := range bundle.StartOrder {
		if bundle.StartOrder[i] != hostPlan.Electron.StartOrder[i] {
			return fmt.Errorf("electron start order mismatch at index %d", i)
		}
	}
	for i := range bundle.ShutdownOrder {
		if bundle.ShutdownOrder[i] != hostPlan.Electron.ShutdownOrder[i] {
			return fmt.Errorf("electron shutdown order mismatch at index %d", i)
		}
	}
	return nil
}

func validateOrderSet(label string, order []string, processes map[string]ProcessManifest) error {
	if len(order) != len(processes) {
		return fmt.Errorf("%s count mismatch: order=%d processes=%d", label, len(order), len(processes))
	}
	seen := make(map[string]struct{}, len(order))
	for _, name := range order {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return fmt.Errorf("%s contains empty process name", label)
		}
		if _, ok := processes[trimmed]; !ok {
			return fmt.Errorf("%s contains unknown process %q", label, trimmed)
		}
		if _, exists := seen[trimmed]; exists {
			return fmt.Errorf("%s contains duplicate process %q", label, trimmed)
		}
		seen[trimmed] = struct{}{}
	}
	return nil
}
