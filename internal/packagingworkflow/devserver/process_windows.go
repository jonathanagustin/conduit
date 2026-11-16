package devserver

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
func interruptProcess(process *os.Process) error {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")
	if err := proc.Find(); err != nil {
		return err
	}
	result, _, err := proc.Call(uintptr(windows.CTRL_BREAK_EVENT), uintptr(process.Pid))
	if result == 0 {
		return fmt.Errorf("interrupt Temporal devserver failed: %v", err)
	}
	return nil
}
