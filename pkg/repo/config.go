package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UserConfig stores user identity for commits.
type UserConfig struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// Config stores repository-local settings such as named remotes.
type Config struct {
	Remotes map[string]string   `json:"remotes,omitempty"`
	User    *UserConfig         `json:"user,omitempty"`
	Hooks   *HookSecurityConfig `json:"hooks,omitempty"`
}

// HookSecurityConfig stores repo-local trust for executable/declarative hooks.
type HookSecurityConfig struct {
	Trusted   bool   `json:"trusted"`
	TrustedAt string `json:"trustedAt,omitempty"`
}

func (r *Repo) configPath() string {
	return filepath.Join(r.GraftDir, "config.json")
}

// ReadConfig reads .graft/config.json. Missing config returns an empty config.
func (r *Repo) ReadConfig() (*Config, error) {
	data, err := os.ReadFile(r.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Remotes: make(map[string]string)}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("read config: unmarshal: %w", err)
	}
	if cfg.Remotes == nil {
		cfg.Remotes = make(map[string]string)
	}
	return &cfg, nil
}

// WriteConfig atomically writes .graft/config.json.
func (r *Repo) WriteConfig(cfg *Config) error {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.Remotes == nil {
		cfg.Remotes = make(map[string]string)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("write config: marshal: %w", err)
	}

	data = append(data, '\n')
	if err := writeFileAtomic(r.configPath(), data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// SetRemote stores/updates a named remote URL in repository config.
func (r *Repo) SetRemote(name, remoteURL string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("set remote: remote name is required")
	}
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return fmt.Errorf("set remote: remote URL is required")
	}

	cfg, err := r.ReadConfig()
	if err != nil {
		return err
	}
	cfg.Remotes[name] = remoteURL
	return r.WriteConfig(cfg)
}

// RemoteURL returns the configured URL for the given remote name.
func (r *Repo) RemoteURL(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("remote name is required")
	}

	cfg, err := r.ReadConfig()
	if err != nil {
		return "", err
	}
	url, ok := cfg.Remotes[name]
	if !ok || strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("remote %q is not configured", name)
	}
	return url, nil
}

// SetHooksTrusted records whether repo-provided hooks may execute.
func (r *Repo) SetHooksTrusted(trusted bool) error {
	cfg, err := r.ReadConfig()
	if err != nil {
		return err
	}
	if cfg.Hooks == nil {
		cfg.Hooks = &HookSecurityConfig{}
	}
	cfg.Hooks.Trusted = trusted
	if trusted {
		cfg.Hooks.TrustedAt = time.Now().UTC().Format(time.RFC3339)
	} else {
		cfg.Hooks.TrustedAt = ""
	}
	return r.WriteConfig(cfg)
}

// HooksTrusted reports whether repo-provided hooks may execute.
func (r *Repo) HooksTrusted() (bool, error) {
	cfg, err := r.ReadConfig()
	if err != nil {
		return false, err
	}
	return cfg.Hooks != nil && cfg.Hooks.Trusted, nil
}
