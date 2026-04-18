package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	helperEnvEnabled    = "DEERFLOW_SANDBOX_HELPER"
	helperEnvBackend    = "DEERFLOW_SANDBOX_BACKEND"
	helperEnvDir        = "DEERFLOW_SANDBOX_DIR"
	helperEnvCmd        = "DEERFLOW_SANDBOX_CMD"
	defaultTimeout      = 5 * time.Minute
	defaultCleanupDelay = 250 * time.Millisecond
)

type backend string

const (
	backendAuto              backend = "auto"
	backendDirect            backend = "direct"
	backendBwrap             backend = "bwrap"
	backendLandlock          backend = "landlock"
	backendWSL2              backend = "wsl2"
	backendWindowsRestricted backend = "windows-restricted"
)

type ExecutionBackend string

const (
	ExecutionBackendAuto              ExecutionBackend = "auto"
	ExecutionBackendDirect            ExecutionBackend = "direct"
	ExecutionBackendBwrap             ExecutionBackend = "bwrap"
	ExecutionBackendLandlock          ExecutionBackend = "landlock"
	ExecutionBackendWSL2              ExecutionBackend = "wsl2"
	ExecutionBackendWindowsRestricted ExecutionBackend = "windows-restricted"
)

type Config struct {
	Timeout          time.Duration
	MaxInstances     int
	CleanupDelay     time.Duration
	ExecutionBackend ExecutionBackend
	AllowedCommands  []string
}

type Session interface {
	Exec(context.Context, string, time.Duration) (*Result, error)
	WriteFile(string, []byte) error
	ReadFile(string) ([]byte, error)
	Close() error
	GetDir() string
}

type TimeoutError struct {
	Duration time.Duration
	Message  string
}

func (e *TimeoutError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("%s after %s", e.Message, e.Duration)
	}
	return fmt.Sprintf("sandbox timed out after %s", e.Duration)
}

// Sandbox isolates commands and files inside a per-session directory.
type Sandbox struct {
	sessionDir string
	processes  []*os.Process

	mu              sync.Mutex
	backend         backend
	cfg             Config
	allowedCommands map[string]struct{}
}

var _ Session = (*Sandbox)(nil)

var defaultWindowsRestrictedAllowedCommands = []string{
	"echo",
	"type",
	"dir",
	"find",
	"findstr",
	"where",
	"more",
	"python",
	"python.exe",
	"python3",
	"py",
	"pip",
	"pip3",
	"go",
	"go.exe",
	"git",
	"git.exe",
	"node",
	"node.exe",
	"npm",
	"npm.cmd",
	"npx",
	"npx.cmd",
	"pnpm",
	"pnpm.cmd",
	"yarn",
	"yarn.cmd",
	"curl",
	"curl.exe",
	"wget",
	"wget.exe",
	"tar",
	"tar.exe",
	"zip",
	"unzip",
	"rg",
	"rg.exe",
}

func init() {
	if os.Getenv(helperEnvEnabled) != "1" {
		return
	}
	os.Exit(runHelper())
}

// New creates a session directory below baseDir and selects the best available backend.
func New(sessionID string, baseDir string) (*Sandbox, error) {
	return NewWithConfig(sessionID, baseDir, Config{})
}

// NewWithConfig creates a session directory below baseDir and applies sandbox settings.
func NewWithConfig(sessionID string, baseDir string, cfg Config) (*Sandbox, error) {
	sessionID = strings.TrimSpace(sessionID)
	baseDir = strings.TrimSpace(baseDir)
	if sessionID == "" {
		return nil, errors.New("sessionID is required")
	}
	if baseDir == "" {
		return nil, errors.New("baseDir is required")
	}

	sessionDir := filepath.Join(baseDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}

	cfg = normalizeConfig(cfg)
	sb := &Sandbox{
		sessionDir:      sessionDir,
		backend:         resolveBackend(sessionDir, normalizeExecutionBackend(cfg.ExecutionBackend)),
		cfg:             cfg,
		allowedCommands: buildCommandAllowSet(cfg.AllowedCommands),
	}

	return sb, nil
}

// Exec executes a shell command inside the sandbox backend.
func (s *Sandbox) Exec(ctx context.Context, cmd string, timeout time.Duration) (*Result, error) {
	if s == nil {
		return nil, errors.New("sandbox is nil")
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil, errors.New("cmd is required")
	}
	if timeout <= 0 {
		timeout = s.cfg.Timeout
	}
	if err := s.validateCommandPolicy(cmd); err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now()
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve helper executable: %w", err)
	}

	command := exec.CommandContext(runCtx, exePath)
	command.Dir = s.sessionDir
	command.SysProcAttr = newSysProcAttr()
	command.Env = append(os.Environ(),
		helperEnvEnabled+"=1",
		helperEnvBackend+"="+string(s.backend),
		helperEnvDir+"="+s.sessionDir,
		helperEnvCmd+"="+cmd,
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start sandbox command: %w", err)
	}

	s.trackProcess(command.Process)
	defer s.untrackProcess(command.Process)

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- command.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-runCtx.Done():
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			log.Printf("sandbox warning: session=%s timeout=%s cmd=%q", filepath.Base(s.sessionDir), timeout, cmd)
		}
		s.forceKill(command.Process)
		waitErr = s.waitAfterKill(waitDone)
	}

	result := &Result{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: exitCode(command.ProcessState, waitErr),
		duration: time.Since(started),
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		timeoutErr := &TimeoutError{
			Duration: timeout,
			Message:  "sandbox execution timed out",
		}
		result.err = timeoutErr
		return result, timeoutErr
	}

	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		result.err = waitErr
		return result, waitErr
	}

	if waitErr != nil {
		result.err = waitErr
	}

	return result, nil
}

// WriteFile writes data under the sandbox session directory.
func (s *Sandbox) WriteFile(path string, data []byte) error {
	resolved, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// ReadFile reads data from the sandbox session directory.
func (s *Sandbox) ReadFile(path string) ([]byte, error) {
	resolved, err := s.resolvePath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return data, nil
}

// Close terminates tracked processes and removes the session directory.
func (s *Sandbox) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	processes := append([]*os.Process(nil), s.processes...)
	s.processes = nil
	s.mu.Unlock()

	for _, proc := range processes {
		if proc == nil {
			continue
		}
		s.forceKill(proc)
	}

	if delay := s.cfg.CleanupDelay; delay > 0 {
		time.Sleep(delay)
	}

	if err := os.RemoveAll(s.sessionDir); err != nil {
		return fmt.Errorf("remove session directory: %w", err)
	}
	return nil
}

// GetDir returns the sandbox session directory.
func (s *Sandbox) GetDir() string {
	if s == nil {
		return ""
	}
	return s.sessionDir
}

func (s *Sandbox) resolvePath(path string) (string, error) {
	if s == nil {
		return "", errors.New("sandbox is nil")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}

	if filepath.IsAbs(path) {
		path = strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
	}
	resolved := filepath.Join(s.sessionDir, path)
	resolved = filepath.Clean(resolved)

	relative, err := filepath.Rel(s.sessionDir, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes sandbox: %s", path)
	}
	return resolved, nil
}

func (s *Sandbox) trackProcess(proc *os.Process) {
	if proc == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processes = append(s.processes, proc)
}

func (s *Sandbox) forceKill(proc *os.Process) {
	forceKillProcess(proc)
}

func (s *Sandbox) waitAfterKill(waitDone <-chan error) error {
	if s == nil || s.cfg.CleanupDelay <= 0 {
		return <-waitDone
	}

	select {
	case err := <-waitDone:
		return err
	case <-time.After(s.cfg.CleanupDelay):
		return context.DeadlineExceeded
	}
}

func (s *Sandbox) untrackProcess(proc *os.Process) {
	if proc == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, candidate := range s.processes {
		if candidate != nil && candidate.Pid == proc.Pid {
			s.processes = append(s.processes[:i], s.processes[i+1:]...)
			return
		}
	}
}

func exitCode(state *os.ProcessState, waitErr error) int {
	if state != nil {
		return state.ExitCode()
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func normalizeConfig(cfg Config) Config {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.CleanupDelay <= 0 {
		cfg.CleanupDelay = defaultCleanupDelay
	}
	cfg.ExecutionBackend = ExecutionBackend(normalizeExecutionBackend(cfg.ExecutionBackend))
	if normalizeExecutionBackend(cfg.ExecutionBackend) == backendWindowsRestricted && len(cfg.AllowedCommands) == 0 {
		cfg.AllowedCommands = append([]string(nil), defaultWindowsRestrictedAllowedCommands...)
	}
	cfg.AllowedCommands = normalizeAllowedCommands(cfg.AllowedCommands)
	return cfg
}

func resolveBackend(sessionDir string, requested backend) backend {
	switch requested {
	case backendDirect, backendWSL2, backendWindowsRestricted:
		return requested
	case backendBwrap:
		if probeBubblewrap(sessionDir) == nil {
			return backendBwrap
		}
		return backendDirect
	case backendLandlock:
		if CheckLandlockAvailable() {
			if err := probeLandlock(sessionDir); err == nil {
				return backendLandlock
			}
		}
		return backendDirect
	default:
		if CheckLandlockAvailable() {
			if err := probeLandlock(sessionDir); err == nil {
				return backendLandlock
			}
		}
		if probeBubblewrap(sessionDir) == nil {
			return backendBwrap
		}
		return backendDirect
	}
}

func normalizeExecutionBackend(value ExecutionBackend) backend {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(ExecutionBackendDirect):
		return backendDirect
	case string(ExecutionBackendBwrap):
		return backendBwrap
	case string(ExecutionBackendLandlock):
		return backendLandlock
	case string(ExecutionBackendWSL2):
		return backendWSL2
	case string(ExecutionBackendWindowsRestricted):
		return backendWindowsRestricted
	default:
		return backendAuto
	}
}

func normalizeAllowedCommands(commands []string) []string {
	if len(commands) == 0 {
		return nil
	}
	unique := make([]string, 0, len(commands))
	seen := map[string]struct{}{}
	for _, raw := range commands {
		normalized := canonicalCommandName(raw)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		unique = append(unique, normalized)
	}
	return unique
}

func buildCommandAllowSet(commands []string) map[string]struct{} {
	if len(commands) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		normalized := canonicalCommandName(cmd)
		if normalized == "" {
			continue
		}
		allowed[normalized] = struct{}{}
	}
	return allowed
}

func (s *Sandbox) validateCommandPolicy(command string) error {
	if s == nil {
		return errors.New("sandbox is nil")
	}
	if s.backend != backendWindowsRestricted {
		return nil
	}
	if len(s.allowedCommands) == 0 {
		return fmt.Errorf("windows-restricted sandbox has empty command allowlist")
	}
	name := primaryCommandName(command)
	if name == "" {
		return fmt.Errorf("windows-restricted sandbox could not resolve command: %q", command)
	}
	if _, ok := s.allowedCommands[name]; ok {
		return nil
	}
	return fmt.Errorf("windows-restricted sandbox command %q is not allowed", name)
}

func primaryCommandName(command string) string {
	token, rest := splitCommandToken(strings.TrimSpace(command))
	name := canonicalCommandName(token)
	if name == "" {
		return ""
	}
	if name == "cmd" || name == "cmd.exe" {
		flagToken, remainder := splitCommandToken(rest)
		flag := strings.ToLower(strings.TrimSpace(flagToken))
		if flag == "/c" || flag == "/k" {
			return primaryCommandName(remainder)
		}
		return primaryCommandName(rest)
	}
	return name
}

func splitCommandToken(value string) (token string, rest string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	if value[0] == '"' || value[0] == '\'' {
		quote := value[0]
		for i := 1; i < len(value); i++ {
			if value[i] == quote {
				return value[1:i], strings.TrimSpace(value[i+1:])
			}
		}
		return strings.Trim(value, "\"'"), ""
	}
	for i := 0; i < len(value); i++ {
		if value[i] == ' ' || value[i] == '\t' || value[i] == '\n' || value[i] == '\r' {
			return value[:i], strings.TrimSpace(value[i+1:])
		}
	}
	return value, ""
}

func canonicalCommandName(value string) string {
	trimmed := strings.Trim(strings.TrimSpace(value), "\"'")
	if trimmed == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(trimmed))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func runHelper() int {
	dir := os.Getenv(helperEnvDir)
	cmd := os.Getenv(helperEnvCmd)
	selectedBackend := backend(os.Getenv(helperEnvBackend))

	if strings.TrimSpace(dir) == "" || strings.TrimSpace(cmd) == "" {
		_, _ = io.WriteString(os.Stderr, "sandbox helper missing configuration\n")
		return 2
	}

	if err := os.Chdir(dir); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "sandbox helper chdir: %v\n", err)
		return 2
	}

	env := helperEnv(os.Environ(), dir)

	switch selectedBackend {
	case backendLandlock:
		if err := applyLandlock(dir); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "landlock setup failed: %v\n", err)
			return runHelperCommand(backendBwrap, dir, cmd, env)
		}
		return runHelperCommand(backendDirect, dir, cmd, env)
	case backendBwrap:
		return runHelperCommand(backendBwrap, dir, cmd, env)
	default:
		return runHelperCommand(selectedBackend, dir, cmd, env)
	}
}

func helperEnv(base []string, dir string) []string {
	filtered := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if strings.HasPrefix(entry, helperEnvEnabled+"=") ||
			strings.HasPrefix(entry, helperEnvBackend+"=") ||
			strings.HasPrefix(entry, helperEnvDir+"=") ||
			strings.HasPrefix(entry, helperEnvCmd+"=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, "HOME="+dir, "PWD="+dir)
	return filtered
}

// ExecDirect runs a command without sandbox restrictions (fallback).
func ExecDirect(ctx context.Context, cmd string, timeout time.Duration) (*Result, error) {
	return ExecDirectInDir(ctx, cmd, "", timeout)
}

// ExecDirectInDir runs a command without sandbox restrictions from the provided directory.
func ExecDirectInDir(ctx context.Context, cmd string, dir string, timeout time.Duration) (*Result, error) {
	start := time.Now()
	execCmd := shellCommand(cmd)
	if strings.TrimSpace(dir) != "" {
		execCmd.Dir = dir
	}

	done := make(chan struct{})
	var buf bytes.Buffer
	execCmd.Stdout = &buf
	execCmd.Stderr = &buf

	if err := execCmd.Start(); err != nil {
		return NewResult("", err.Error(), -1, time.Since(start), err), nil
	}

	go func() {
		select {
		case <-ctx.Done():
			execCmd.Process.Kill()
		case <-done:
		}
	}()

	err := execCmd.Wait()
	close(done)
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return NewResult(buf.String(), "", exitCode, duration, nil), nil
}
