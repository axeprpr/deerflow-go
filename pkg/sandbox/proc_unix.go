//go:build !windows

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func newSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func forceKillProcess(proc *os.Process) {
	if proc == nil {
		return
	}
	if proc.Pid > 0 {
		_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
	}
	_ = proc.Kill()
}

func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("/bin/sh", "-lc", command)
	cmd.SysProcAttr = newSysProcAttr()
	return cmd
}

func runHelperCommand(selectedBackend backend, dir string, command string, env []string) int {
	if selectedBackend == backendBwrap {
		args, err := bubblewrapArgs(dir, command)
		if err == nil {
			if err := syscall.Exec(bwrapPath, args, env); err == nil {
				return 0
			}
		}
	}
	args := []string{"/bin/sh", "-lc", command}
	if err := syscall.Exec("/bin/sh", args, env); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "exec /bin/sh: %v\n", err)
		return 127
	}
	return 0
}
