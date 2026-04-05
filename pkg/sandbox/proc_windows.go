//go:build windows

package sandbox

import (
	"os"
	"os/exec"
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

func runHelperCommand(_ backend, _ string, command string, env []string) int {
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
