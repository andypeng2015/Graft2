//go:build windows

package repo

import "os"

func processRunningPlatform(pid int) bool {
	if pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return true
	}
	return true
}
