package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	RepositoryFormatVersion          = 1
	RepositoryConfigSchemaVersion    = 1
	DefaultRepositoryObjectHash      = "sha256"
	repositoryConfigFileName         = "config"
	repositoryConfigCreatedByDefault = "graft"
)

// RepositoryConfig records the on-disk repository format and enabled
// repository-level features. It is stored at .graft/config.
type RepositoryConfig struct {
	SchemaVersion           int             `json:"schema_version"`
	RepositoryFormatVersion int             `json:"repository_format_version"`
	CreatedBy               string          `json:"created_by"`
	CreatedAt               string          `json:"created_at"`
	ObjectHash              string          `json:"object_hash"`
	Features                map[string]bool `json:"features,omitempty"`
}

type RepositoryConfigMigrationResult struct {
	Path        string `json:"path"`
	Migrated    bool   `json:"migrated"`
	FromVersion int    `json:"from_version"`
	ToVersion   int    `json:"to_version"`
}

func defaultRepositoryConfig(now time.Time) *RepositoryConfig {
	return &RepositoryConfig{
		SchemaVersion:           RepositoryConfigSchemaVersion,
		RepositoryFormatVersion: RepositoryFormatVersion,
		CreatedBy:               repositoryConfigCreatedByDefault,
		CreatedAt:               now.UTC().Format(time.RFC3339),
		ObjectHash:              DefaultRepositoryObjectHash,
		Features: map[string]bool{
			"coord_refs": true,
			"git_shadow": false,
			"lfs":        true,
			"modules":    true,
		},
	}
}

func (r *Repo) repositoryConfigPath() string {
	base := r.CommonDir
	if base == "" {
		base = r.GraftDir
	}
	return filepath.Join(base, repositoryConfigFileName)
}

// ReadRepositoryConfig reads .graft/config. Missing config is treated as a
// legacy repository so old repositories remain openable.
func (r *Repo) ReadRepositoryConfig() (*RepositoryConfig, error) {
	data, err := os.ReadFile(r.repositoryConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &RepositoryConfig{
				SchemaVersion:           RepositoryConfigSchemaVersion,
				RepositoryFormatVersion: 0,
				CreatedBy:               "legacy",
				ObjectHash:              DefaultRepositoryObjectHash,
				Features:                map[string]bool{},
			}, nil
		}
		return nil, fmt.Errorf("read repository config: %w", err)
	}
	var cfg RepositoryConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("read repository config: unmarshal: %w", err)
	}
	if cfg.Features == nil {
		cfg.Features = map[string]bool{}
	}
	return &cfg, nil
}

// WriteRepositoryConfig atomically writes .graft/config.
func (r *Repo) WriteRepositoryConfig(cfg *RepositoryConfig) error {
	if cfg == nil {
		cfg = defaultRepositoryConfig(time.Now())
	}
	if cfg.Features == nil {
		cfg.Features = map[string]bool{}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("write repository config: marshal: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(r.repositoryConfigPath(), data, 0o644); err != nil {
		return fmt.Errorf("write repository config: %w", err)
	}
	return nil
}

func (r *Repo) CheckRepositoryFormat() error {
	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		return err
	}
	if cfg.RepositoryFormatVersion > RepositoryFormatVersion {
		return fmt.Errorf(
			"unsupported graft repository format %d (this graft supports up to %d)",
			cfg.RepositoryFormatVersion,
			RepositoryFormatVersion,
		)
	}
	if cfg.ObjectHash != "" && cfg.ObjectHash != DefaultRepositoryObjectHash {
		return fmt.Errorf("unsupported graft object hash %q (expected %q)", cfg.ObjectHash, DefaultRepositoryObjectHash)
	}
	return nil
}

func (r *Repo) MigrateRepositoryConfig() (*RepositoryConfigMigrationResult, error) {
	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		return nil, err
	}
	result := &RepositoryConfigMigrationResult{
		Path:        r.repositoryConfigPath(),
		FromVersion: cfg.RepositoryFormatVersion,
		ToVersion:   RepositoryFormatVersion,
	}
	if cfg.RepositoryFormatVersion > 0 {
		return result, nil
	}
	if err := r.WriteRepositoryConfig(defaultRepositoryConfig(time.Now())); err != nil {
		return nil, err
	}
	result.Migrated = true
	return result, nil
}

// SetRepositoryFeature updates a feature flag in .graft/config.
func (r *Repo) SetRepositoryFeature(name string, enabled bool) error {
	cfg, err := r.ReadRepositoryConfig()
	if err != nil {
		return err
	}
	if cfg.RepositoryFormatVersion == 0 {
		cfg = defaultRepositoryConfig(time.Now())
	}
	if cfg.Features == nil {
		cfg.Features = map[string]bool{}
	}
	cfg.Features[name] = enabled
	return r.WriteRepositoryConfig(cfg)
}
