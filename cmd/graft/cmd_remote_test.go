package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestRemoteCmdJSONRedactsAndSortsRemotes(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := r.SetRemote("origin", "https://alice:secret@example.com/graft/alice/repo?token=top-secret&ok=yes"); err != nil {
		t.Fatalf("SetRemote(origin): %v", err)
	}
	if err := r.SetRemote("backup", "https://github.com/acme/repo.git"); err != nil {
		t.Fatalf("SetRemote(backup): %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRemoteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	for _, secret := range []string{"secret", "top-secret"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("remote JSON leaked %q:\n%s", secret, raw)
		}
	}

	var result JSONRemoteOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, raw)
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if len(result.Remotes) != 2 {
		t.Fatalf("remotes = %+v, want 2 entries", result.Remotes)
	}
	if result.Remotes[0].Name != "backup" || result.Remotes[0].Transport != "git" {
		t.Fatalf("first remote = %+v, want backup git", result.Remotes[0])
	}
	if result.Remotes[1].Name != "origin" || result.Remotes[1].Transport != "graft" {
		t.Fatalf("second remote = %+v, want origin graft", result.Remotes[1])
	}
	if !strings.Contains(result.Remotes[1].URL, "redacted") {
		t.Fatalf("origin URL was not redacted: %q", result.Remotes[1].URL)
	}
}

func TestRemoteCmdJSONInvalidRemoteDoesNotLeakRawURL(t *testing.T) {
	result := remoteConfigToJSON(map[string]string{
		"bad": "not a url with token=top-secret",
	})
	if len(result.Remotes) != 1 {
		t.Fatalf("remotes = %+v, want one entry", result.Remotes)
	}
	entry := result.Remotes[0]
	if entry.Transport != "unknown" {
		t.Fatalf("transport = %q, want unknown", entry.Transport)
	}
	if entry.Warning == "" {
		t.Fatalf("warning empty for invalid remote: %+v", entry)
	}
	if strings.Contains(entry.Warning, "top-secret") {
		t.Fatalf("warning leaked secret: %q", entry.Warning)
	}
}

func TestRemoteCmdHumanOutputStillUsesRawURLs(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	rawURL := "https://alice:secret@example.com/graft/alice/repo"
	if err := r.SetRemote("origin", rawURL); err != nil {
		t.Fatalf("SetRemote(origin): %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRemoteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "origin\t"+rawURL {
		t.Fatalf("remote output = %q, want raw URL", got)
	}
}

func TestRemoteAddRejectsInsecureHTTPNonLocal(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRemoteCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"add", "origin", "http://example.com/graft/alice/repo"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("remote add succeeded, want insecure HTTP failure")
	}
	if !strings.Contains(err.Error(), "insecure HTTP") || !strings.Contains(err.Error(), "--allow-insecure") {
		t.Fatalf("error = %q, want insecure HTTP guidance", err.Error())
	}
}

func TestRemoteAddAllowsLocalHTTP(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newRemoteCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"add", "origin", "http://127.0.0.1:8080/graft/alice/repo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("remote add Execute: %v", err)
	}
	got, err := r.RemoteURL("origin")
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	if got != "http://127.0.0.1:8080/graft/alice/repo" {
		t.Fatalf("remote URL = %q", got)
	}
}

func TestRemoteAddAllowInsecureOverride(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newRemoteCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"add", "--allow-insecure", "origin", "http://example.com/graft/alice/repo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("remote add Execute: %v", err)
	}
	got, err := r.RemoteURL("origin")
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	if got != "http://example.com/graft/alice/repo" {
		t.Fatalf("remote URL = %q", got)
	}
}

func TestRemoteJSONWarnsOnInsecureHTTP(t *testing.T) {
	result := remoteConfigToJSON(map[string]string{
		"origin": "http://example.com/graft/alice/repo",
	})
	if len(result.Remotes) != 1 {
		t.Fatalf("remotes = %+v, want one entry", result.Remotes)
	}
	if !strings.Contains(result.Remotes[0].Warning, "insecure HTTP") {
		t.Fatalf("warning = %q, want insecure HTTP warning", result.Remotes[0].Warning)
	}
}
