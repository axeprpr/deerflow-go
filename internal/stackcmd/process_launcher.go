package stackcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ProcessLaunchOptions struct {
	Stdout            io.Writer
	Stderr            io.Writer
	BinaryDir         string
	RestartPolicy     ProcessRestartPolicy
	MaxRestarts       int
	RestartDelay      time.Duration
	DependencyTimeout time.Duration
	FailureIsolation  bool
}

type ProcessRestartPolicy string

const (
	ProcessRestartNever     ProcessRestartPolicy = "never"
	ProcessRestartOnFailure ProcessRestartPolicy = "on-failure"
	ProcessRestartAlways    ProcessRestartPolicy = "always"
)

type ProcessLauncher struct {
	processes        []ProcessManifest
	lifecycles       []*processLifecycle
	order            []string
	failureIsolation bool
}

type processLifecycle struct {
	name   string
	binary string
	args   []string
	stdout io.Writer
	stderr io.Writer

	dependencies      []string
	dependencyTimeout time.Duration
	restartPolicy     ProcessRestartPolicy
	maxRestarts       int
	restartDelay      time.Duration

	mu            sync.Mutex
	cmd           *exec.Cmd
	done          chan struct{}
	waitErr       error
	stopRequested bool
}

func NewProcessLauncher(processes []ProcessManifest, options ProcessLaunchOptions) (*ProcessLauncher, error) {
	order, err := resolveProcessOrder(processes)
	if err != nil {
		return nil, err
	}
	processByName := make(map[string]ProcessManifest, len(processes))
	for _, process := range processes {
		processByName[strings.TrimSpace(process.Name)] = process
	}

	restartPolicy, err := parseProcessRestartPolicy(string(options.RestartPolicy))
	if err != nil {
		return nil, err
	}
	if options.DependencyTimeout <= 0 {
		options.DependencyTimeout = 60 * time.Second
	}
	if options.RestartDelay < 0 {
		options.RestartDelay = 0
	}

	lifecycles := make([]*processLifecycle, 0, len(processes))
	for _, name := range order {
		process := processByName[name]
		dependencyURLs, err := dependencyReadyURLs(process, processByName)
		if err != nil {
			return nil, err
		}
		lifecycle, err := newProcessLifecycle(process, dependencyURLs, options, restartPolicy)
		if err != nil {
			return nil, err
		}
		lifecycles = append(lifecycles, lifecycle)
	}
	return &ProcessLauncher{
		processes:        append([]ProcessManifest(nil), processes...),
		lifecycles:       lifecycles,
		order:            append([]string(nil), order...),
		failureIsolation: options.FailureIsolation,
	}, nil
}

func newProcessLifecycle(process ProcessManifest, dependencyURLs []string, options ProcessLaunchOptions, restartPolicy ProcessRestartPolicy) (*processLifecycle, error) {
	name := strings.TrimSpace(process.Name)
	if name == "" {
		return nil, fmt.Errorf("process name is required")
	}
	binary := strings.TrimSpace(process.Binary)
	if binary == "" {
		return nil, fmt.Errorf("process %q binary is required", name)
	}
	if dir := strings.TrimSpace(options.BinaryDir); dir != "" && !filepath.IsAbs(binary) {
		binary = filepath.Join(dir, binary)
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("process %q binary %q is not executable: %w", name, binary, err)
	}
	return &processLifecycle{
		name:              name,
		binary:            resolved,
		args:              append([]string(nil), process.Args...),
		stdout:            options.Stdout,
		stderr:            options.Stderr,
		dependencies:      append([]string(nil), dependencyURLs...),
		dependencyTimeout: options.DependencyTimeout,
		restartPolicy:     restartPolicy,
		maxRestarts:       options.MaxRestarts,
		restartDelay:      options.RestartDelay,
	}, nil
}

func (l *ProcessLauncher) Processes() []ProcessManifest {
	if l == nil {
		return nil
	}
	return append([]ProcessManifest(nil), l.processes...)
}

func (l *ProcessLauncher) Start() error {
	if l == nil || len(l.lifecycles) == 0 {
		return nil
	}
	runners := make([]processRunner, 0, len(l.lifecycles))
	for _, lifecycle := range l.lifecycles {
		runners = append(runners, lifecycle)
	}
	return runProcessRunners(runners, l.failureIsolation)
}

func (l *ProcessLauncher) Close(ctx context.Context) error {
	if l == nil {
		return nil
	}
	var closeErr error
	for i := len(l.lifecycles) - 1; i >= 0; i-- {
		if err := l.lifecycles[i].Close(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

type processRunner interface {
	Start() error
	Close(context.Context) error
}

func runProcessRunners(runners []processRunner, failureIsolation bool) error {
	if len(runners) == 0 {
		return nil
	}
	errCh := make(chan error, len(runners))
	for _, runner := range runners {
		item := runner
		go func() {
			errCh <- item.Start()
		}()
	}

	var firstErr error
	for range runners {
		err := <-errCh
		if err == nil {
			continue
		}
		if !failureIsolation {
			_ = closeProcessRunners(runners, context.Background())
			return err
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func closeProcessRunners(runners []processRunner, ctx context.Context) error {
	var closeErr error
	for i := len(runners) - 1; i >= 0; i-- {
		if err := runners[i].Close(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (p *processLifecycle) Start() error {
	if p == nil {
		return nil
	}
	if err := p.waitForDependencies(); err != nil {
		return err
	}

	restarts := 0
	for {
		cmd, err := p.prepareCommand()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			p.markWait(err, true)
			return fmt.Errorf("process %q start failed: %w", p.name, err)
		}
		waitErr := cmd.Wait()
		stopping := p.markWait(waitErr, true)
		if stopping {
			return nil
		}
		shouldRestart, exceeded := p.restartDecision(waitErr, restarts)
		if !shouldRestart {
			if exceeded {
				if waitErr != nil {
					return fmt.Errorf("process %q exceeded restart limit %d: %w", p.name, p.maxRestarts, waitErr)
				}
				return fmt.Errorf("process %q exceeded restart limit %d", p.name, p.maxRestarts)
			}
			if waitErr != nil {
				return fmt.Errorf("process %q exited with error: %w", p.name, waitErr)
			}
			return nil
		}
		restarts++
		if p.restartDelay > 0 {
			time.Sleep(p.restartDelay)
		}
	}
}

func (p *processLifecycle) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.stopRequested = true
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if done == nil {
		return nil
	}

	_ = cmd.Process.Signal(os.Interrupt)

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return ctx.Err()
	}
}

func (p *processLifecycle) prepareCommand() (*exec.Cmd, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopRequested {
		return nil, context.Canceled
	}
	if p.cmd != nil {
		return nil, fmt.Errorf("process %q already started", p.name)
	}
	cmd := exec.Command(p.binary, p.args...)
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	p.cmd = cmd
	p.done = make(chan struct{})
	p.waitErr = nil
	return cmd, nil
}

func (p *processLifecycle) markWait(waitErr error, clearCmd bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.waitErr = waitErr
	if p.done != nil {
		close(p.done)
	}
	if clearCmd {
		p.cmd = nil
		p.done = nil
	}
	return p.stopRequested
}

func (p *processLifecycle) waitForDependencies() error {
	if p == nil || len(p.dependencies) == 0 {
		return nil
	}
	var deadline time.Time
	if p.dependencyTimeout > 0 {
		deadline = time.Now().Add(p.dependencyTimeout)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for _, target := range p.dependencies {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		for {
			p.mu.Lock()
			stopping := p.stopRequested
			p.mu.Unlock()
			if stopping {
				return context.Canceled
			}
			resp, err := client.Get(target)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 500 {
					break
				}
			}
			if !deadline.IsZero() && time.Now().After(deadline) {
				return fmt.Errorf("process %q dependency %s not ready within %s", p.name, target, p.dependencyTimeout)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	return nil
}

func (p *processLifecycle) restartDecision(waitErr error, restarts int) (bool, bool) {
	switch p.restartPolicy {
	case ProcessRestartAlways:
		if p.maxRestarts > 0 && restarts >= p.maxRestarts {
			return false, true
		}
		return true, false
	case ProcessRestartOnFailure:
		if waitErr == nil {
			return false, false
		}
		if p.maxRestarts > 0 && restarts >= p.maxRestarts {
			return false, true
		}
		return true, false
	default:
		return false, false
	}
}

func parseProcessRestartPolicy(raw string) (ProcessRestartPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ProcessRestartOnFailure):
		return ProcessRestartOnFailure, nil
	case string(ProcessRestartNever):
		return ProcessRestartNever, nil
	case string(ProcessRestartAlways):
		return ProcessRestartAlways, nil
	default:
		return "", fmt.Errorf("invalid restart policy %q: want never|on-failure|always", raw)
	}
}

func dependencyReadyURLs(process ProcessManifest, byName map[string]ProcessManifest) ([]string, error) {
	urls := make([]string, 0, len(process.DependsOn))
	for _, dep := range process.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		item, ok := byName[dep]
		if !ok {
			return nil, fmt.Errorf("process %q depends on unknown process %q", strings.TrimSpace(process.Name), dep)
		}
		target := strings.TrimSpace(item.ReadyURL)
		if target == "" {
			return nil, fmt.Errorf("process %q dependency %q is missing ready URL", strings.TrimSpace(process.Name), dep)
		}
		urls = append(urls, target)
	}
	return urls, nil
}

func resolveProcessOrder(processes []ProcessManifest) ([]string, error) {
	manifest := StackManifest{Processes: append([]ProcessManifest(nil), processes...)}
	if err := manifest.ValidateProcessGraph(); err != nil {
		return nil, err
	}
	byName := make(map[string]ProcessManifest, len(processes))
	for _, process := range processes {
		name := strings.TrimSpace(process.Name)
		byName[name] = process
	}

	order := make([]string, 0, len(processes))
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if seen[name] {
			return nil
		}
		process, ok := byName[name]
		if !ok {
			return errors.New("unknown process in order resolution")
		}
		for _, dep := range process.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		seen[name] = true
		order = append(order, name)
		return nil
	}
	for _, process := range processes {
		name := strings.TrimSpace(process.Name)
		if name == "" {
			continue
		}
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}
