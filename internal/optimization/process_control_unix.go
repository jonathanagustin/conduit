//go:build linux || darwin

package optimization

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureExternalProcessCancellation terminates the entire process group on
// supported Unix hosts so a canceled solver cannot leave descendants behind.
func configureExternalProcessCancellation(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
