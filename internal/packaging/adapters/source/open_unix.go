//go:build !windows

package source

import "syscall"

const nonblockFlag = syscall.O_NONBLOCK
