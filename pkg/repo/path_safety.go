package repo

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func validateRepoPath(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.ContainsRune(rel, '\x00') {
		return "", fmt.Errorf("path %q contains NUL byte", rel)
	}
	if strings.Contains(rel, "\\") {
		return "", fmt.Errorf("path %q contains backslash separator", rel)
	}
	if strings.HasPrefix(rel, "/") || looksLikeWindowsAbsPath(rel) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q is absolute", rel)
	}

	clean := path.Clean(filepath.ToSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q is outside repository", rel)
	}
	if clean != filepath.ToSlash(rel) {
		return "", fmt.Errorf("path %q is not clean", rel)
	}

	for _, segment := range strings.Split(clean, "/") {
		if err := validateRepoPathElement(segment); err != nil {
			return "", fmt.Errorf("path %q: %w", rel, err)
		}
	}
	return clean, nil
}

func validateRepoPathElement(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid path segment %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("path segment %q contains a separator", name)
	}
	if strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("path segment %q contains NUL byte", name)
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("path segment %q is not portable across supported filesystems", name)
	}
	if isReservedWindowsPathElement(name) {
		return fmt.Errorf("path segment %q is reserved on Windows", name)
	}
	return nil
}

func validateRepoPathSet(paths []string) error {
	seenFolded := make(map[string]string, len(paths))
	for _, rel := range paths {
		clean, err := validateRepoPath(rel)
		if err != nil {
			return err
		}
		folded := strings.ToLower(clean)
		if existing, ok := seenFolded[folded]; ok && existing != clean {
			return fmt.Errorf("path %q conflicts with %q on case-insensitive filesystems", clean, existing)
		}
		seenFolded[folded] = clean
	}
	return nil
}

func validateStagingPaths(stg *Staging) error {
	if stg == nil {
		return fmt.Errorf("nil staging")
	}
	paths := make([]string, 0, len(stg.Entries))
	for key, entry := range stg.Entries {
		if entry == nil {
			return fmt.Errorf("staging entry %q is nil", key)
		}
		cleanKey, err := validateRepoPath(key)
		if err != nil {
			return fmt.Errorf("invalid staging path %q: %w", key, err)
		}
		if strings.TrimSpace(entry.Path) == "" {
			entry.Path = cleanKey
		}
		cleanEntry, err := validateRepoPath(entry.Path)
		if err != nil {
			return fmt.Errorf("invalid staging entry path %q: %w", entry.Path, err)
		}
		if cleanKey != cleanEntry {
			return fmt.Errorf("staging entry key %q does not match entry path %q", key, entry.Path)
		}
		paths = append(paths, cleanKey)
	}
	return validateRepoPathSet(paths)
}

func safeTreePath(prefix, name string) (string, error) {
	if err := validateRepoPathElement(name); err != nil {
		return "", err
	}
	if prefix == "" {
		return name, nil
	}
	full := prefix + "/" + name
	if _, err := validateRepoPath(full); err != nil {
		return "", err
	}
	return full, nil
}

func safeModuleTreePath(prefix, name string) (string, error) {
	full := name
	if prefix != "" {
		full = prefix + "/" + name
	}
	return validateRepoPath(full)
}

func looksLikeWindowsAbsPath(p string) bool {
	if len(p) >= 3 && isASCIIAlpha(p[0]) && p[1] == ':' && (p[2] == '/' || p[2] == '\\') {
		return true
	}
	return strings.HasPrefix(p, "//") || strings.HasPrefix(p, `\\`)
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isReservedWindowsPathElement(name string) bool {
	base := strings.TrimRight(name, " .")
	if idx := strings.IndexByte(base, '.'); idx >= 0 {
		base = base[:idx]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		upper := strings.ToUpper(base[:3])
		if (upper == "COM" || upper == "LPT") && base[3] >= '1' && base[3] <= '9' {
			return true
		}
	}
	return false
}

func rejectSymlinkPath(pathForUser, absPath string) error {
	info, err := os.Lstat(absPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path %q is a symlink; graft does not stage symlink targets", pathForUser)
	}
	return nil
}

func ensureSafeParentDir(rootDir, absPath string) error {
	dir := filepath.Dir(absPath)
	rel, err := filepath.Rel(rootDir, dir)
	if err != nil {
		return fmt.Errorf("resolve parent %q: %w", dir, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return nil
	}
	if isOutsideRepo(rel) {
		return fmt.Errorf("parent directory %q is outside repository", dir)
	}

	current := rootDir
	for _, segment := range strings.Split(rel, "/") {
		if segment == "" || segment == "." {
			continue
		}
		current = filepath.Join(current, filepath.FromSlash(segment))
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
				return fmt.Errorf("mkdir %q: %w", current, err)
			}
			continue
		}
		if statErr != nil {
			return fmt.Errorf("stat parent %q: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlinked parent directory %q", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("parent path %q exists and is not a directory", current)
		}
	}
	return nil
}

func (r *Repo) safeWorktreePath(rel string) (string, error) {
	clean, err := validateRepoPath(rel)
	if err != nil {
		return "", err
	}
	return filepath.Join(r.RootDir, filepath.FromSlash(clean)), nil
}
