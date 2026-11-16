//go:build !windows

package devserver

import (
	"os"
	"os/exec"
)

func configureProcess(*exec.Cmd)                 {}
func interruptProcess(process *os.Process) error { return process.Signal(os.Interrupt) }
