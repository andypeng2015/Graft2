//go:build !windows

package repo

import (
	"errors"
	"os"
	"syscall"
)

func processRunningPlatform(pid int) bool {
	if pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return true
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
