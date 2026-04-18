package stackcmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	processHelperEnabledEnv      = "DEERFLOW_PROCESS_HELPER"
	processHelperAddrPrimaryEnv  = "DEERFLOW_HELPER_ADDR_PRIMARY"
	processHelperAddrSecondEnv   = "DEERFLOW_HELPER_ADDR_SECONDARY"
	processHelperReadyPrimaryEnv = "DEERFLOW_HELPER_READY_FILE_PRIMARY"
	processHelperReadySecondEnv  = "DEERFLOW_HELPER_READY_FILE_SECONDARY"
	processHelperDelayMsEnv      = "DEERFLOW_HELPER_DELAY_MS_PRIMARY"
	processHelperCounterFileEnv  = "DEERFLOW_HELPER_COUNTER_FILE"
)

func TestProcessLauncherRestartsFailingProcessUntilReady(t *testing.T) {
	t.Setenv(processHelperEnabledEnv, "1")

	addr := freeTCPAddr(t)
	workdir := t.TempDir()
	counterFile := filepath.Join(workdir, "restart-count.txt")

	t.Setenv(processHelperAddrPrimaryEnv, addr)
	t.Setenv(processHelperCounterFileEnv, counterFile)

	binDir := installTestProcessBinaries(t, "worker-bin")
	launcher, err := NewProcessLauncher([]ProcessManifest{
		{
			Name:     "worker",
			Binary:   "worker-bin",
			Args:     helperProcessArgs("TestProcessLauncherHelperFailOnceThenServe"),
			ReadyURL: httpURL(addr) + "/healthz",
		},
	}, ProcessLaunchOptions{
		BinaryDir:         binDir,
		RestartPolicy:     ProcessRestartOnFailure,
		MaxRestarts:       2,
		RestartDelay:      50 * time.Millisecond,
		DependencyTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewProcessLauncher() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- launcher.Start()
	}()

	waitForHTTP(t, httpURL(addr)+"/healthz")
	if got := waitForCounterAtLeast(t, counterFile, 2, 3*time.Second); got < 2 {
		t.Fatalf("restart counter = %d, want >= 2", got)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launcher.Close(shutdownCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestProcessLauncherDependencyGateBlocksDependentStartUntilReady(t *testing.T) {
	t.Setenv(processHelperEnabledEnv, "1")

	delayMs := 350
	stateAddr := freeTCPAddr(t)
	workerAddr := freeTCPAddr(t)
	workdir := t.TempDir()
	stateReadyFile := filepath.Join(workdir, "state-ready.txt")
	workerReadyFile := filepath.Join(workdir, "worker-ready.txt")

	t.Setenv(processHelperAddrPrimaryEnv, stateAddr)
	t.Setenv(processHelperAddrSecondEnv, workerAddr)
	t.Setenv(processHelperDelayMsEnv, strconv.Itoa(delayMs))
	t.Setenv(processHelperReadyPrimaryEnv, stateReadyFile)
	t.Setenv(processHelperReadySecondEnv, workerReadyFile)

	binDir := installTestProcessBinaries(t, "state-bin", "worker-bin")
	launcher, err := NewProcessLauncher([]ProcessManifest{
		{
			Name:     "state",
			Binary:   "state-bin",
			Args:     helperProcessArgs("TestProcessLauncherHelperServeDelayedPrimary"),
			ReadyURL: httpURL(stateAddr) + "/healthz",
		},
		{
			Name:      "worker",
			Binary:    "worker-bin",
			Args:      helperProcessArgs("TestProcessLauncherHelperServeSecondary"),
			ReadyURL:  httpURL(workerAddr) + "/healthz",
			DependsOn: []string{"state"},
		},
	}, ProcessLaunchOptions{
		BinaryDir:         binDir,
		RestartPolicy:     ProcessRestartNever,
		DependencyTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewProcessLauncher() error = %v", err)
	}

	errCh := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		errCh <- launcher.Start()
	}()

	time.Sleep(120 * time.Millisecond)
	if _, err := os.Stat(workerReadyFile); err == nil {
		t.Fatalf("worker ready file exists before dependency is expected ready: %s", workerReadyFile)
	}

	waitForHTTP(t, httpURL(stateAddr)+"/healthz")
	waitForHTTP(t, httpURL(workerAddr)+"/healthz")
	if elapsed := time.Since(startedAt); elapsed < 250*time.Millisecond {
		t.Fatalf("dependent process started too early: elapsed=%s", elapsed)
	}

	stateInfo, err := os.Stat(stateReadyFile)
	if err != nil {
		t.Fatalf("os.Stat(stateReadyFile) error = %v", err)
	}
	workerInfo, err := os.Stat(workerReadyFile)
	if err != nil {
		t.Fatalf("os.Stat(workerReadyFile) error = %v", err)
	}
	if workerInfo.ModTime().Before(stateInfo.ModTime()) {
		t.Fatalf("worker ready before state: worker=%s state=%s", workerInfo.ModTime(), stateInfo.ModTime())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launcher.Close(shutdownCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestProcessLauncherFailureIsolationKeepsHealthyProcessRunningUntilClose(t *testing.T) {
	t.Setenv(processHelperEnabledEnv, "1")

	addr := freeTCPAddr(t)
	t.Setenv(processHelperAddrPrimaryEnv, addr)

	binDir := installTestProcessBinaries(t, "healthy-bin", "failing-bin")
	launcher, err := NewProcessLauncher([]ProcessManifest{
		{
			Name:     "healthy",
			Binary:   "healthy-bin",
			Args:     helperProcessArgs("TestProcessLauncherHelperServePrimary"),
			ReadyURL: httpURL(addr) + "/healthz",
		},
		{
			Name:   "failing",
			Binary: "failing-bin",
			Args:   helperProcessArgs("TestProcessLauncherHelperFailFast"),
		},
	}, ProcessLaunchOptions{
		BinaryDir:        binDir,
		RestartPolicy:    ProcessRestartNever,
		FailureIsolation: true,
	})
	if err != nil {
		t.Fatalf("NewProcessLauncher() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- launcher.Start()
	}()

	waitForHTTP(t, httpURL(addr)+"/healthz")
	select {
	case err := <-errCh:
		t.Fatalf("Start() returned too early with isolation enabled: %v", err)
	case <-time.After(120 * time.Millisecond):
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := launcher.Close(shutdownCtx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err = <-errCh
	if err == nil {
		t.Fatal("Start() error = nil, want failing process error")
	}
	if !strings.Contains(err.Error(), `process "failing"`) {
		t.Fatalf("Start() error = %v, want failing process error", err)
	}
}

func TestProcessLauncherHelperServePrimary(t *testing.T) {
	if !isProcessHelperMode() {
		t.Skip("process helper mode disabled")
	}
	if err := runProcessHelperServer(processHelperAddrPrimaryEnv, processHelperReadyPrimaryEnv, 0); err != nil {
		t.Fatalf("runProcessHelperServer(primary) error = %v", err)
	}
}

func TestProcessLauncherHelperServeSecondary(t *testing.T) {
	if !isProcessHelperMode() {
		t.Skip("process helper mode disabled")
	}
	if err := runProcessHelperServer(processHelperAddrSecondEnv, processHelperReadySecondEnv, 0); err != nil {
		t.Fatalf("runProcessHelperServer(secondary) error = %v", err)
	}
}

func TestProcessLauncherHelperServeDelayedPrimary(t *testing.T) {
	if !isProcessHelperMode() {
		t.Skip("process helper mode disabled")
	}
	delayMs, err := strconv.Atoi(strings.TrimSpace(os.Getenv(processHelperDelayMsEnv)))
	if err != nil || delayMs <= 0 {
		t.Fatalf("invalid %s=%q", processHelperDelayMsEnv, os.Getenv(processHelperDelayMsEnv))
	}
	if err := runProcessHelperServer(processHelperAddrPrimaryEnv, processHelperReadyPrimaryEnv, time.Duration(delayMs)*time.Millisecond); err != nil {
		t.Fatalf("runProcessHelperServer(delayed primary) error = %v", err)
	}
}

func TestProcessLauncherHelperFailOnceThenServe(t *testing.T) {
	if !isProcessHelperMode() {
		t.Skip("process helper mode disabled")
	}
	counterPath := strings.TrimSpace(os.Getenv(processHelperCounterFileEnv))
	if counterPath == "" {
		t.Fatalf("missing %s", processHelperCounterFileEnv)
	}
	counter, err := incrementCounter(counterPath)
	if err != nil {
		t.Fatalf("incrementCounter(%s) error = %v", counterPath, err)
	}
	if counter == 1 {
		t.Fatal("intentional first failure")
	}
	if err := runProcessHelperServer(processHelperAddrPrimaryEnv, processHelperReadyPrimaryEnv, 0); err != nil {
		t.Fatalf("runProcessHelperServer(fail-once) error = %v", err)
	}
}

func TestProcessLauncherHelperFailFast(t *testing.T) {
	if !isProcessHelperMode() {
		t.Skip("process helper mode disabled")
	}
	t.Fatal("intentional helper failure")
}

func helperProcessArgs(helperTest string) []string {
	return []string{"-test.run=^" + strings.TrimSpace(helperTest) + "$"}
}

func isProcessHelperMode() bool {
	return strings.TrimSpace(os.Getenv(processHelperEnabledEnv)) == "1"
}

func runProcessHelperServer(addrEnv string, readyFileEnv string, startupDelay time.Duration) error {
	addr := strings.TrimSpace(os.Getenv(addrEnv))
	if addr == "" {
		return fmt.Errorf("missing %s", addrEnv)
	}
	if startupDelay > 0 {
		time.Sleep(startupDelay)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()
	if readyFile := strings.TrimSpace(os.Getenv(readyFileEnv)); readyFile != "" {
		if err := os.WriteFile(readyFile, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o644); err != nil {
			return err
		}
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-sigCtx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func incrementCounter(path string) (int, error) {
	current := 0
	if data, err := os.ReadFile(path); err == nil {
		text := strings.TrimSpace(string(data))
		if text != "" {
			value, parseErr := strconv.Atoi(text)
			if parseErr != nil {
				return 0, parseErr
			}
			current = value
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	current++
	if err := os.WriteFile(path, []byte(strconv.Itoa(current)), 0o644); err != nil {
		return 0, err
	}
	return current, nil
}

func waitForCounterAtLeast(t *testing.T, path string, min int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := 0
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			text := strings.TrimSpace(string(data))
			if text != "" {
				if value, parseErr := strconv.Atoi(text); parseErr == nil {
					last = value
					if value >= min {
						return value
					}
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return last
}
