package repo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// Test 1: Init creates .graft/ structure (HEAD, objects/, refs/heads/).
func TestInit_CreatesStructure(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init(%q): %v", dir, err)
	}
	if r.RootDir != dir {
		t.Errorf("RootDir = %q, want %q", r.RootDir, dir)
	}

	graftDir := filepath.Join(dir, ".graft")
	if r.GraftDir != graftDir {
		t.Errorf("GraftDir = %q, want %q", r.GraftDir, graftDir)
	}

	// .graft/ directory exists
	assertDir(t, graftDir)

	// HEAD file exists
	assertFile(t, filepath.Join(graftDir, "HEAD"))

	// objects/ directory exists
	assertDir(t, filepath.Join(graftDir, "objects"))

	// refs/heads/ directory exists
	assertDir(t, filepath.Join(graftDir, "refs", "heads"))
	assertDir(t, filepath.Join(graftDir, "logs", "refs", "heads"))

	// Store is non-nil
	if r.Store == nil {
		t.Error("Store is nil after Init")
	}

	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		t.Fatalf("ReadRepositoryConfig: %v", err)
	}
	if cfg.RepositoryFormatVersion != RepositoryFormatVersion {
		t.Fatalf("repository_format_version = %d, want %d", cfg.RepositoryFormatVersion, RepositoryFormatVersion)
	}
	if cfg.ObjectHash != DefaultRepositoryObjectHash {
		t.Fatalf("object_hash = %q, want %q", cfg.ObjectHash, DefaultRepositoryObjectHash)
	}
	if cfg.CreatedBy == "" || cfg.CreatedAt == "" {
		t.Fatalf("created_by/created_at should be populated: %+v", cfg)
	}
}

func TestMigrateRepositoryConfigCreatesMissingConfig(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := os.Remove(filepath.Join(r.GraftDir, "config")); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	report := r.VerifyIntegrity()
	if !hasRepositoryDiagnostic(report, "repository_config_missing") {
		t.Fatalf("missing repository_config_missing diagnostic: %+v", report.Diagnostics)
	}

	result, err := r.MigrateRepositoryConfig()
	if err != nil {
		t.Fatalf("MigrateRepositoryConfig: %v", err)
	}
	if !result.Migrated {
		t.Fatal("Migrated = false, want true")
	}
	if result.FromVersion != 0 || result.ToVersion != RepositoryFormatVersion {
		t.Fatalf("migration versions = %d -> %d, want 0 -> %d", result.FromVersion, result.ToVersion, RepositoryFormatVersion)
	}
	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		t.Fatalf("ReadRepositoryConfig: %v", err)
	}
	if cfg.RepositoryFormatVersion != RepositoryFormatVersion {
		t.Fatalf("repository_format_version = %d, want %d", cfg.RepositoryFormatVersion, RepositoryFormatVersion)
	}
	report = r.VerifyIntegrity()
	if hasRepositoryDiagnostic(report, "repository_config_missing") {
		t.Fatalf("repository_config_missing still present: %+v", report.Diagnostics)
	}
}

// Test 2: Init on existing repo returns error.
func TestInit_ExistingRepo_Error(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(dir)
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}

	_, err = Init(dir)
	if err == nil {
		t.Fatal("second Init should fail on existing repo, got nil error")
	}
}

// Test 3: Open finds .graft/ from subdirectory.
func TestOpen_FromSubdirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	sub := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	r, err := Open(sub)
	if err != nil {
		t.Fatalf("Open(%q): %v", sub, err)
	}

	if r.RootDir != dir {
		t.Errorf("RootDir = %q, want %q", r.RootDir, dir)
	}
	if r.GraftDir != filepath.Join(dir, ".graft") {
		t.Errorf("GraftDir = %q, want %q", r.GraftDir, filepath.Join(dir, ".graft"))
	}
	if r.Store == nil {
		t.Error("Store is nil after Open")
	}
}

// Test 4: Open in non-repo directory returns error.
func TestOpen_NoRepo_Error(t *testing.T) {
	dir := t.TempDir()

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open should fail in non-repo directory, got nil error")
	}
}

func TestOpen_RejectsUnsupportedRepositoryFormat(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		t.Fatalf("ReadRepositoryConfig: %v", err)
	}
	cfg.RepositoryFormatVersion = RepositoryFormatVersion + 1
	if err := r.WriteRepositoryConfig(cfg); err != nil {
		t.Fatalf("WriteRepositoryConfig: %v", err)
	}

	if _, err := Open(dir); err == nil {
		t.Fatal("Open should reject unsupported newer repository format")
	}
}

func TestOpen_RejectsUnsupportedObjectHash(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		t.Fatalf("ReadRepositoryConfig: %v", err)
	}
	cfg.ObjectHash = "sha512"
	if err := r.WriteRepositoryConfig(cfg); err != nil {
		t.Fatalf("WriteRepositoryConfig: %v", err)
	}

	if _, err := Open(dir); err == nil {
		t.Fatal("Open should reject unsupported object hash")
	} else if !strings.Contains(err.Error(), `unsupported graft object hash "sha512"`) {
		t.Fatalf("Open error = %v, want unsupported object hash", err)
	}
}

func TestReadRepositoryConfig_LegacyMissingConfig(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := os.Remove(filepath.Join(r.GraftDir, repositoryConfigFileName)); err != nil {
		t.Fatalf("remove config: %v", err)
	}

	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		t.Fatalf("ReadRepositoryConfig: %v", err)
	}
	if cfg.RepositoryFormatVersion != 0 {
		t.Fatalf("legacy repository_format_version = %d, want 0", cfg.RepositoryFormatVersion)
	}
	if _, err := Open(dir); err != nil {
		t.Fatalf("Open should allow missing legacy repository config: %v", err)
	}
}

func TestRepositoryConfig_OnDiskJSON(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(r.GraftDir, repositoryConfigFileName))
	if err != nil {
		t.Fatalf("ReadFile(config): %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf(".graft/config is not JSON: %v\n%s", err, data)
	}
	if raw["repository_format_version"] == nil {
		t.Fatalf(".graft/config missing repository_format_version: %s", data)
	}
}

// Test 5: HEAD defaults to "ref: refs/heads/main".
func TestInit_HeadDefault(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	ref, err := r.Head()
	if err != nil {
		t.Fatalf("Head(): %v", err)
	}
	if ref != "refs/heads/main" {
		t.Errorf("Head() = %q, want %q", ref, "refs/heads/main")
	}
}

// Test 6: UpdateRef + ResolveRef round-trip.
func TestUpdateRef_ResolveRef_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	if err := r.UpdateRef("refs/heads/main", h); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	got, err := r.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != h {
		t.Errorf("ResolveRef = %q, want %q", got, h)
	}
}

func TestUpdateRef_InitPathReflogFailureIsExplicit(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	logDir := filepath.Join(r.GraftDir, "logs", "refs", "heads")
	if err := os.Remove(logDir); err != nil {
		t.Fatalf("remove reflog dir: %v", err)
	}
	if err := os.WriteFile(logDir, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("create reflog path blocker: %v", err)
	}

	h := object.Hash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	err = r.UpdateRef("refs/heads/main", h)
	if err == nil {
		t.Fatal("UpdateRef should fail when reflog append fails, got nil")
	}
	if !errors.Is(err, ErrRefUpdatedButReflogAppendFailed) {
		t.Fatalf("UpdateRef error = %v, want ErrRefUpdatedButReflogAppendFailed", err)
	}

	got, resolveErr := r.ResolveRef("refs/heads/main")
	if resolveErr != nil {
		t.Fatalf("ResolveRef(main): %v", resolveErr)
	}
	if got != h {
		t.Fatalf("ResolveRef(main) = %q, want %q", got, h)
	}
}

// Test 7: ResolveRef with HEAD pointing to a branch that has a hash.
func TestResolveRef_HEAD_FollowsBranch(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// HEAD points to refs/heads/main by default, so write hash to that ref.
	if err := r.UpdateRef("refs/heads/main", h); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	got, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if got != h {
		t.Errorf("ResolveRef(HEAD) = %q, want %q", got, h)
	}
}

// Test 8: ResolveRef short name (e.g., "main" resolves via refs/heads/main).
func TestResolveRef_ShortName(t *testing.T) {
	dir := t.TempDir()

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h := object.Hash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")

	if err := r.UpdateRef("refs/heads/main", h); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	got, err := r.ResolveRef("main")
	if err != nil {
		t.Fatalf("ResolveRef(main): %v", err)
	}
	if got != h {
		t.Errorf("ResolveRef(main) = %q, want %q", got, h)
	}
}

// helpers

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("expected directory %q to exist: %v", path, err)
		return
	}
	if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", path)
	}
}

func assertFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("expected file %q to exist: %v", path, err)
		return
	}
	if info.IsDir() {
		t.Errorf("%q exists but is a directory, expected file", path)
	}
}
