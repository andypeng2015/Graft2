package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
)

func TestIntegration_ConfigSetGetRepoLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Set user.name at repo level.
	mustRunGraft(t, dir, "config", "user.name", "Alice")

	// Get user.name.
	out := mustRunGraft(t, dir, "config", "user.name")
	if got := strings.TrimSpace(out); got != "Alice" {
		t.Fatalf("config user.name = %q, want %q", got, "Alice")
	}

	// Set user.email at repo level.
	mustRunGraft(t, dir, "config", "user.email", "alice@example.com")

	// Get user.email.
	out = mustRunGraft(t, dir, "config", "user.email")
	if got := strings.TrimSpace(out); got != "alice@example.com" {
		t.Fatalf("config user.email = %q, want %q", got, "alice@example.com")
	}

	// List should show both.
	out = mustRunGraft(t, dir, "config", "--list")
	if !strings.Contains(out, "user.name=Alice") {
		t.Fatalf("config --list missing user.name: %s", out)
	}
	if !strings.Contains(out, "user.email=alice@example.com") {
		t.Fatalf("config --list missing user.email: %s", out)
	}
}

func TestIntegration_ConfigGlobalSetGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Use a temp HOME to avoid polluting real config.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dir := initRepo(t)

	// Set global user.name.
	mustRunGraft(t, dir, "config", "--global", "user.name", "GlobalAlice")

	// Get global user.name.
	out := mustRunGraft(t, dir, "config", "--global", "user.name")
	if got := strings.TrimSpace(out); got != "GlobalAlice" {
		t.Fatalf("config --global user.name = %q, want %q", got, "GlobalAlice")
	}

	// Verify it was written to ~/.graftconfig.
	data, err := os.ReadFile(filepath.Join(fakeHome, ".graftconfig"))
	if err != nil {
		t.Fatalf("read .graftconfig: %v", err)
	}
	if !strings.Contains(string(data), "GlobalAlice") {
		t.Fatalf(".graftconfig missing GlobalAlice: %s", string(data))
	}
}

func TestConfigGlobalOrchardAndSigningKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	runConfigCommand(t, "--global", "orchard.url", "orchard.example.com")
	runConfigCommand(t, "--global", "orchard.username", "alice")
	runConfigCommand(t, "--global", "orchard.owner", "acme")
	runConfigCommand(t, "--global", "signing.key", filepath.Join(home, ".ssh", "id_ed25519"))
	runConfigCommand(t, "--global", "signing.auto", "true")

	if got := strings.TrimSpace(runConfigCommand(t, "--global", "orchard.url")); got != "https://orchard.example.com" {
		t.Fatalf("orchard.url = %q, want normalized https://orchard.example.com", got)
	}
	if got := strings.TrimSpace(runConfigCommand(t, "--global", "orchard.username")); got != "alice" {
		t.Fatalf("orchard.username = %q, want alice", got)
	}
	if got := strings.TrimSpace(runConfigCommand(t, "--global", "orchard.owner")); got != "acme" {
		t.Fatalf("orchard.owner = %q, want acme", got)
	}
	if got := strings.TrimSpace(runConfigCommand(t, "--global", "signing.auto")); got != "true" {
		t.Fatalf("signing.auto = %q, want true", got)
	}

	out := runConfigCommand(t, "--global", "--list")
	for _, want := range []string{
		"orchard.url=https://orchard.example.com",
		"orchard.username=alice",
		"orchard.owner=acme",
		"signing.key=" + filepath.Join(home, ".ssh", "id_ed25519"),
		"signing.auto=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("config --global --list missing %q:\n%s", want, out)
		}
	}
}

func TestConfigGlobalSigningAutoRejectsInvalidBool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newConfigCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--global", "signing.auto", "sometimes"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid signing.auto value to fail")
	}
}

func TestConfigHelpDocumentsHookTrustFlow(t *testing.T) {
	var out bytes.Buffer
	cmd := newConfigCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config --help: %v", err)
	}
	raw := out.String()
	for _, want := range []string{
		"Hook trust:",
		"Cloned or imported repositories mark repo-provided hooks untrusted",
		"graft config hooks.trusted true",
		"graft config hooks.trusted false",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("config help missing %q:\n%s", want, raw)
		}
	}
}

func TestConfigRepoAutoInitNoticeUsesCommandErrorWriter(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")

	restore := chdirForTest(t, dir)
	defer restore()

	var errOut bytes.Buffer
	cmd := newConfigCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"hooks.trusted", "true"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config hooks.trusted: %v\nstderr: %s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), ".graft not found") {
		t.Fatalf("stderr = %q, want auto-init notice", errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".graft", "config.json")); err != nil {
		t.Fatalf("config.json not written: %v", err)
	}
}

func TestIntegration_ConfigFallbackToGlobal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dir := initRepo(t)

	// Set global user.name (no repo-level config).
	mustRunGraft(t, dir, "config", "--global", "user.name", "FallbackAlice")

	// Get user.name without --global — should fall back to global.
	out := mustRunGraft(t, dir, "config", "user.name")
	if got := strings.TrimSpace(out); got != "FallbackAlice" {
		t.Fatalf("config user.name (fallback) = %q, want %q", got, "FallbackAlice")
	}
}

func TestIntegration_CommitWithConfigAuthor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Set repo-level author identity.
	mustRunGraft(t, dir, "config", "user.name", "Config Author")
	mustRunGraft(t, dir, "config", "user.email", "config@example.com")

	// Create and commit a file WITHOUT --author.
	writeFile(t, dir, "hello.txt", "hello world\n")
	mustRunGraft(t, dir, "add", "hello.txt")
	commitOut := mustRunGraft(t, dir, "commit", "-m", "config author test", "--no-sign")

	if !strings.Contains(commitOut, "config author test") {
		t.Errorf("commit output missing message: %s", commitOut)
	}

	// Log should show the config author.
	logOut := mustRunGraft(t, dir, "log")
	if !strings.Contains(logOut, "Config Author") {
		t.Errorf("log should show config author: %s", logOut)
	}
	if !strings.Contains(logOut, "config@example.com") {
		t.Errorf("log should show config email: %s", logOut)
	}
}

func TestIntegration_ConfigUnknownKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Unknown key should fail.
	_, err := runGraft(t, dir, "config", "foo.bar", "value")
	if err == nil {
		t.Fatal("expected error for unknown config key")
	}
}

func TestFormatUserConfigIncludesOrchardProfiles(t *testing.T) {
	lines := formatUserConfig(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Username:   "orchard-user",
		Owner:      "orchard-owner",
		OrchardProfiles: map[string]userconfig.OrchardProfile{
			"https://orchard.example.com": {
				Token:    "orchard-token",
				Username: "orchard-user",
				Owner:    "orchard-owner",
			},
			"https://code.example.com/api/v1": {
				Token:    "code-token",
				Username: "code-user",
				Owner:    "code-owner",
			},
		},
	})

	out := strings.Join(lines, "\n")
	for _, want := range []string{
		"orchard.url=https://orchard.example.com",
		"orchard.username=orchard-user",
		"orchard.owner=orchard-owner",
		"orchard.profile[https://orchard.example.com].default=true",
		"orchard.profile[https://orchard.example.com].token=set",
		"orchard.profile[https://code.example.com/api/v1].username=code-user",
		"orchard.profile[https://code.example.com/api/v1].owner=code-owner",
		"orchard.profile[https://code.example.com/api/v1].token=set",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatUserConfig output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "code-token") || strings.Contains(out, "orchard-token") {
		t.Fatalf("formatUserConfig leaked token values:\n%s", out)
	}
}

func runConfigCommand(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := newConfigCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config %v: %v", args, err)
	}
	return out.String()
}

func TestRepoConfigHooksTrustedKey(t *testing.T) {
	cfg := &repo.Config{}
	if err := applyRepoConfigKey(cfg, "hooks.trusted", "true"); err != nil {
		t.Fatalf("apply hooks.trusted: %v", err)
	}
	got, err := readRepoConfigKey(cfg, "hooks.trusted")
	if err != nil {
		t.Fatalf("read hooks.trusted: %v", err)
	}
	if got != "true" {
		t.Fatalf("hooks.trusted = %q, want true", got)
	}
	lines := strings.Join(formatRepoConfig(cfg), "\n")
	if !strings.Contains(lines, "hooks.trusted=true") {
		t.Fatalf("formatRepoConfig missing hooks.trusted=true:\n%s", lines)
	}

	if err := applyRepoConfigKey(cfg, "hooks.trusted", "not-bool"); err == nil {
		t.Fatal("expected invalid hooks.trusted value to fail")
	}
}
