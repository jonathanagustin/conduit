//go:build windows

package optimization

import "os/exec"

// configureExternalProcessCancellation retains exec.CommandContext's native
// Process.Kill cancellation on Windows. Process-tree containment remains a
// distribution/runtime responsibility, as documented by the adapter contract.
func configureExternalProcessCancellation(command *exec.Cmd) {
	// CommandContext already installs the supported Windows cancellation hook.
}
