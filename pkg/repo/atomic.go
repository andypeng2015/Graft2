package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// writeFileAtomic writes data to path using same-directory temp+fsync+rename
// semantics, then fsyncs the parent directory on platforms that support it.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("atomic write mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("atomic write tmpfile %s: %w", path, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomic write chmod %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomic write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomic write sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomic write close %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomic write rename %s: %w", path, err)
	}
	cleanup = false
	if err := fsyncParentDir(path); err != nil {
		return fmt.Errorf("atomic write sync parent %s: %w", path, err)
	}
	return nil
}

func appendFileAtomic(path string, line []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data := make([]byte, 0, len(existing)+len(line))
	data = append(data, existing...)
	data = append(data, line...)
	return writeFileAtomic(path, data, perm)
}

func fsyncParentDir(path string) error {
	return fsyncDir(filepath.Dir(path))
}

func fsyncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !isUnsupportedFsyncErr(err) {
		return err
	}
	return nil
}

func isUnsupportedFsyncErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTSUP) ||
		strings.Contains(strings.ToLower(err.Error()), "invalid argument") ||
		strings.Contains(strings.ToLower(err.Error()), "operation not supported")
}
