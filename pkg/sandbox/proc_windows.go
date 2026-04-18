//go:build windows

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func newSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func forceKillProcess(proc *os.Process) {
	if proc == nil {
		return
	}
	_ = proc.Kill()
}

func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("cmd", "/C", command)
	cmd.SysProcAttr = newSysProcAttr()
	return cmd
}

func runHelperCommand(selectedBackend backend, dir string, command string, env []string) int {
	if selectedBackend == backendWSL2 {
		return runWSLHelperCommand(dir, command, env)
	}
	cmd := exec.Command("cmd", "/C", command)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 127
	}
	return 0
}

func runWSLHelperCommand(dir string, command string, env []string) int {
	wslDir, ok := windowsPathToWSL(dir)
	if !ok {
		_, _ = fmt.Fprintf(os.Stderr, "wsl2 sandbox path conversion failed: %s\n", dir)
		return 127
	}
	script := fmt.Sprintf("cd %s && %s", singleQuoteForBash(wslDir), command)
	cmd := exec.Command("wsl.exe", "--", "bash", "-lc", script)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 127
	}
	return 0
}

func windowsPathToWSL(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if len(path) < 3 {
		return "", false
	}
	clean := filepath.Clean(path)
	if len(clean) < 3 || clean[1] != ':' {
		return "", false
	}
	drive := strings.ToLower(string(clean[0]))
	rest := strings.TrimPrefix(clean[2:], "\\")
	rest = strings.ReplaceAll(rest, "\\", "/")
	if strings.TrimSpace(rest) == "" {
		return "/mnt/" + drive, true
	}
	return "/mnt/" + drive + "/" + rest, true
}

func singleQuoteForBash(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
