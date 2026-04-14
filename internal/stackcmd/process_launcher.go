package stackcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/axeprpr/deerflow-go/internal/commandrun"
)

type ProcessLaunchOptions struct {
	Stdout    io.Writer
	Stderr    io.Writer
	BinaryDir string
}

type ProcessLauncher struct {
	group      *commandrun.LifecycleGroup
	processes  []ProcessManifest
	lifecycles []*processLifecycle
}

type processLifecycle struct {
	name   string
	binary string
	args   []string
	stdout io.Writer
	stderr io.Writer

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
}

func NewProcessLauncher(processes []ProcessManifest, options ProcessLaunchOptions) (*ProcessLauncher, error) {
	lifecycles := make([]*processLifecycle, 0, len(processes))
	items := make([]commandrun.Lifecycle, 0, len(processes))
	for _, process := range processes {
		lifecycle, err := newProcessLifecycle(process, options)
		if err != nil {
			return nil, err
		}
		lifecycles = append(lifecycles, lifecycle)
		items = append(items, lifecycle)
	}
	return &ProcessLauncher{
		group:      commandrun.NewLifecycleGroup(items...),
		processes:  append([]ProcessManifest(nil), processes...),
		lifecycles: lifecycles,
	}, nil
}

func newProcessLifecycle(process ProcessManifest, options ProcessLaunchOptions) (*processLifecycle, error) {
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
		name:   name,
		binary: resolved,
		args:   append([]string(nil), process.Args...),
		stdout: options.Stdout,
		stderr: options.Stderr,
	}, nil
}

func (l *ProcessLauncher) Processes() []ProcessManifest {
	if l == nil {
		return nil
	}
	return append([]ProcessManifest(nil), l.processes...)
}

func (l *ProcessLauncher) Start() error {
	if l == nil || l.group == nil {
		return nil
	}
	return l.group.Start()
}

func (l *ProcessLauncher) Close(ctx context.Context) error {
	if l == nil || l.group == nil {
		return nil
	}
	return l.group.Close(ctx)
}

func (p *processLifecycle) Start() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.cmd != nil {
		p.mu.Unlock()
		return fmt.Errorf("process %q already started", p.name)
	}
	cmd := exec.Command(p.binary, p.args...)
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	p.cmd = cmd
	p.done = make(chan struct{})
	p.waitErr = nil
	p.mu.Unlock()

	if err := cmd.Start(); err != nil {
		p.mu.Lock()
		p.waitErr = err
		close(p.done)
		p.mu.Unlock()
		return fmt.Errorf("process %q start failed: %w", p.name, err)
	}

	waitErr := cmd.Wait()
	p.mu.Lock()
	p.waitErr = waitErr
	close(p.done)
	p.mu.Unlock()
	if waitErr != nil {
		return fmt.Errorf("process %q exited with error: %w", p.name, waitErr)
	}
	return nil
}

func (p *processLifecycle) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
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
