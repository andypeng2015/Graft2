package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	RepositoryLockSchemaVersion = 1

	repositoryLocksDir  = "locks"
	repositoryLockName  = "repository.lock"
	repositoryLockRetry = 25 * time.Millisecond
	repositoryLockTTL   = 15 * time.Minute
)

type RepositoryLockInfo struct {
	SchemaVersion int    `json:"schema_version"`
	Operation     string `json:"operation"`
	PID           int    `json:"pid"`
	Hostname      string `json:"hostname"`
	Command       string `json:"command"`
	StartedAt     string `json:"started_at"`
}

type RepositoryLockStatus struct {
	Path    string
	Exists  bool
	Stale   bool
	Reason  string
	Age     time.Duration
	Info    RepositoryLockInfo
	RawData []byte
}

type RepositoryLock struct {
	repo *Repo
	path string
}

func (r *Repo) WithRepositoryLock(operation string, fn func() error) error {
	if fn == nil {
		return nil
	}
	if r.enterNestedRepositoryLock() {
		defer r.exitNestedRepositoryLock()
		return fn()
	}

	lock, err := r.AcquireRepositoryLock(operation)
	if err != nil {
		return err
	}
	r.repositoryLockMu.Lock()
	r.repositoryLock = lock
	r.repositoryLockDepth = 1
	r.repositoryLockMu.Unlock()

	fnErr := fn()
	releaseErr := r.releaseRepositoryLock()
	if fnErr != nil {
		return fnErr
	}
	return releaseErr
}

func (r *Repo) withRepositoryLock(operation string, fn func() error) error {
	return r.WithRepositoryLock(operation, fn)
}

func (r *Repo) enterNestedRepositoryLock() bool {
	r.repositoryLockMu.Lock()
	defer r.repositoryLockMu.Unlock()
	if r.repositoryLockDepth == 0 {
		return false
	}
	r.repositoryLockDepth++
	return true
}

func (r *Repo) exitNestedRepositoryLock() {
	r.repositoryLockMu.Lock()
	defer r.repositoryLockMu.Unlock()
	if r.repositoryLockDepth > 0 {
		r.repositoryLockDepth--
	}
}

func (r *Repo) releaseRepositoryLock() error {
	r.repositoryLockMu.Lock()
	if r.repositoryLockDepth > 0 {
		r.repositoryLockDepth--
	}
	if r.repositoryLockDepth > 0 {
		r.repositoryLockMu.Unlock()
		return nil
	}
	lock := r.repositoryLock
	r.repositoryLock = nil
	r.repositoryLockMu.Unlock()
	if lock == nil {
		return nil
	}
	return lock.Release()
}

func (r *Repo) AcquireRepositoryLock(operation string) (*RepositoryLock, error) {
	operation = sanitizeRepositoryLockOperation(operation)
	lockPath := r.repositoryLockPath()
	deadline := time.Now().Add(repositoryLockWaitLimit())

	for {
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
			return nil, fmt.Errorf("repository lock: mkdir: %w", err)
		}

		f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			if writeErr := writeRepositoryLockFile(f, repositoryLockInfo(operation)); writeErr != nil {
				_ = f.Close()
				_ = os.Remove(lockPath)
				return nil, writeErr
			}
			if err := f.Close(); err != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("repository lock: close: %w", err)
			}
			if err := fsyncParentDir(lockPath); err != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("repository lock: sync parent: %w", err)
			}
			return &RepositoryLock{repo: r, path: lockPath}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("repository lock: acquire: %w", err)
		}

		status, statusErr := r.RepositoryLockStatus()
		if statusErr != nil {
			return nil, statusErr
		}
		if status.Exists && status.Stale {
			if removeErr := os.Remove(lockPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return nil, fmt.Errorf("repository lock: remove stale lock: %w", removeErr)
			}
			_ = fsyncParentDir(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, repositoryLockedError(status)
		}
		time.Sleep(repositoryLockRetry)
	}
}

func (l *RepositoryLock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	err := os.Remove(l.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("repository lock: release: %w", err)
	}
	if err := fsyncParentDir(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("repository lock: release sync parent: %w", err)
	}
	l.path = ""
	return nil
}

func (r *Repo) RepositoryLockStatus() (RepositoryLockStatus, error) {
	path := r.repositoryLockPath()
	status := RepositoryLockStatus{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return status, fmt.Errorf("repository lock: read: %w", err)
	}
	status.Exists = true
	status.RawData = data

	info, parseErr := parseRepositoryLockInfo(data)
	if parseErr != nil {
		status.Stale = true
		status.Reason = parseErr.Error()
		status.Age = repositoryLockFileAge(path)
		return status, nil
	}
	status.Info = info
	status.Age = repositoryLockAge(path, info)
	status.Stale, status.Reason = repositoryLockIsStale(info, status.Age)
	return status, nil
}

func (r *Repo) ClearStaleRepositoryLock() (RepositoryLockStatus, bool, error) {
	status, err := r.RepositoryLockStatus()
	if err != nil || !status.Exists || !status.Stale {
		return status, false, err
	}
	if err := os.Remove(status.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return status, false, fmt.Errorf("repository lock: clear stale lock: %w", err)
	}
	if err := fsyncParentDir(status.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return status, false, fmt.Errorf("repository lock: clear stale lock sync parent: %w", err)
	}
	return status, true, nil
}

func (r *Repo) repositoryLockPath() string {
	base := r.CommonDir
	if base == "" {
		base = r.GraftDir
	}
	return filepath.Join(base, repositoryLocksDir, repositoryLockName)
}

func writeRepositoryLockFile(f *os.File, info RepositoryLockInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("repository lock: marshal: %w", err)
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("repository lock: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("repository lock: sync: %w", err)
	}
	return nil
}

func repositoryLockInfo(operation string) RepositoryLockInfo {
	hostname, _ := os.Hostname()
	return RepositoryLockInfo{
		SchemaVersion: RepositoryLockSchemaVersion,
		Operation:     operation,
		PID:           os.Getpid(),
		Hostname:      hostname,
		Command:       strings.Join(os.Args, " "),
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func parseRepositoryLockInfo(data []byte) (RepositoryLockInfo, error) {
	var info RepositoryLockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return info, fmt.Errorf("malformed repository lock: %w", err)
	}
	if info.SchemaVersion != RepositoryLockSchemaVersion {
		return info, fmt.Errorf("unsupported repository lock schema %d", info.SchemaVersion)
	}
	if strings.TrimSpace(info.Operation) == "" {
		return info, fmt.Errorf("repository lock missing operation")
	}
	if strings.TrimSpace(info.StartedAt) == "" {
		return info, fmt.Errorf("repository lock missing started_at")
	}
	return info, nil
}

func repositoryLockAge(path string, info RepositoryLockInfo) time.Duration {
	startedAt, err := time.Parse(time.RFC3339Nano, info.StartedAt)
	if err == nil {
		return time.Since(startedAt)
	}
	return repositoryLockFileAge(path)
}

func repositoryLockFileAge(path string) time.Duration {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return time.Since(info.ModTime())
}

func repositoryLockIsStale(info RepositoryLockInfo, age time.Duration) (bool, string) {
	if info.PID <= 0 {
		return true, "lock owner PID is not valid"
	}
	hostname, _ := os.Hostname()
	sameHost := info.Hostname == "" || hostname == "" || info.Hostname == hostname
	if sameHost && !processRunning(info.PID) {
		return true, fmt.Sprintf("lock owner process %d is not running", info.PID)
	}
	if age > repositoryLockTTL && sameHost && !processRunning(info.PID) {
		return true, fmt.Sprintf("lock is older than %s and owner process %d is not running", repositoryLockTTL, info.PID)
	}
	return false, ""
}

func processRunning(pid int) bool {
	return processRunningPlatform(pid)
}

func sanitizeRepositoryLockOperation(operation string) string {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return "unknown"
	}
	operation = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == ':' || r == '.':
			return r
		default:
			return '-'
		}
	}, operation)
	return strings.Trim(operation, "-")
}

func repositoryLockWaitLimit() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("GRAFT_LOCK_WAIT_MS")); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err == nil && ms >= 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return refLockWaitLimit
}

func repositoryLockedError(status RepositoryLockStatus) error {
	if !status.Exists {
		return fmt.Errorf("repository lock: timeout waiting for %s", status.Path)
	}
	owner := status.Info
	if owner.Operation == "" {
		return fmt.Errorf("repository is locked by unreadable lock %s; run graft repair clear-stale-locks if no graft process is running", status.Path)
	}
	return fmt.Errorf(
		"repository is locked by %q (pid %d on %s, started %s, command %q); retry later or run graft repair clear-stale-locks if that process is gone",
		owner.Operation,
		owner.PID,
		owner.Hostname,
		owner.StartedAt,
		owner.Command,
	)
}
