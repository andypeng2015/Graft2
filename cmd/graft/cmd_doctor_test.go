package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
)

func TestDoctorCmdJSONCleanRepository(t *testing.T) {
	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONDoctorOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if !result.OK {
		t.Fatalf("ok = false, want true: %+v", result.Diagnostics)
	}
	if result.SchemaVersion == 0 {
		t.Fatal("schemaVersion = 0, want nonzero")
	}
}

func TestDoctorCmdJSONReportsInsecureRemoteWithoutLeakingSecrets(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := r.SetRemote("origin", "http://alice:remote-secret@example.com/graft/alice/repo?token=remote-token&ok=yes"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nraw: %s", err, out.String())
	}

	raw := out.String()
	for _, secret := range []string{"remote-secret", "remote-token"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("doctor leaked %q:\n%s", secret, raw)
		}
	}

	var result JSONDoctorOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, raw)
	}
	if !result.OK {
		t.Fatalf("ok = false, want true for advisory insecure remote warning: %+v", result.Diagnostics)
	}
	diagnostic, ok := doctorOutputDiagnostic(result, "remote_transport_insecure")
	if !ok {
		t.Fatalf("diagnostics missing remote_transport_insecure: %+v", result.Diagnostics)
	}
	if diagnostic.Severity != "warning" {
		t.Fatalf("severity = %q, want warning", diagnostic.Severity)
	}
	if diagnostic.Operation != "remote" {
		t.Fatalf("operation = %q, want remote", diagnostic.Operation)
	}
	if !strings.Contains(diagnostic.Message, "origin") || !strings.Contains(diagnostic.Message, "insecure HTTP") {
		t.Fatalf("unexpected diagnostic message: %q", diagnostic.Message)
	}
	if !strings.Contains(diagnostic.Message, "redacted") {
		t.Fatalf("diagnostic message did not include redacted URL: %q", diagnostic.Message)
	}
	if !strings.Contains(diagnostic.Repair, "graft remote set-url origin") {
		t.Fatalf("repair = %q, want set-url guidance", diagnostic.Repair)
	}
}

func TestDoctorBundleIncludesInsecureRemoteDiagnostic(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := r.SetRemote("origin", "git://example.com/alice/repo"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--bundle"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nraw: %s", err, out.String())
	}

	var result JSONDoctorBundleOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if !result.Verify.OK {
		t.Fatalf("verify ok = false, want true for advisory insecure remote warning: %+v", result.Verify.Diagnostics)
	}
	if !doctorOutputHasCode(result.Verify, "remote_transport_insecure") {
		t.Fatalf("bundle verify diagnostics missing remote_transport_insecure: %+v", result.Verify.Diagnostics)
	}
	if !doctorBundleProtocolHasCode(result.Protocol, "remote_transport_insecure") {
		t.Fatalf("bundle protocol diagnostics missing remote_transport_insecure: %+v", result.Protocol.Diagnostics)
	}
}

func TestDoctorCmdJSONReportsBrokenRef(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	bad := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	refPath := filepath.Join(r.GraftDir, "refs", "heads", "bad")
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(refPath, []byte(string(bad)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(ref): %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor should fail when repository has integrity errors")
	}

	var result JSONDoctorOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK {
		t.Fatal("ok = true, want false")
	}
	if !doctorOutputHasCode(result, "ref_target_unreachable") {
		t.Fatalf("diagnostics missing ref_target_unreachable: %+v", result.Diagnostics)
	}
}

func TestDoctorCmdBundleRedactsSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := userconfig.Save(&userconfig.Config{
		Name:           "Sensitive Name",
		Email:          "sensitive@example.com",
		OrchardURL:     "https://orchard.example.com",
		Token:          "top-secret-token",
		Username:       "sensitive-user",
		Owner:          "sensitive-owner",
		SigningKeyPath: filepath.Join(home, "super-secret-signing-key"),
		AutoSign:       true,
		OrchardProfiles: map[string]userconfig.OrchardProfile{
			"https://code.example.com/api/v1": {
				Token:    "profile-secret-token",
				Username: "profile-user",
				Owner:    "profile-owner",
			},
		},
	}); err != nil {
		t.Fatalf("userconfig.Save: %v", err)
	}

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	remoteURL := "http://alice:remote-secret@example.com/graft/alice/repo?token=remote-token&ok=yes"
	if err := r.SetRemote("origin", remoteURL); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}
	headLog := filepath.Join(dir, ".graft", "logs", "refs", "heads", "main")
	if err := os.MkdirAll(filepath.Dir(headLog), 0o755); err != nil {
		t.Fatalf("MkdirAll HEAD log: %v", err)
	}
	reflogReason := `fetch https://alice:reflog-secret@example.com/repo?token=reflog-token Authorization: Bearer reflog-bearer standalone Bearer reflog-standalone`
	reflogLine := strings.Repeat("0", 64) + " " + strings.Repeat("a", 64) + " 1 " + reflogReason + "\n"
	if err := os.WriteFile(headLog, []byte(reflogLine), 0o644); err != nil {
		t.Fatalf("WriteFile HEAD log: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--bundle"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	for _, secret := range []string{
		"top-secret-token",
		"profile-secret-token",
		"remote-secret",
		"remote-token",
		"reflog-secret",
		"reflog-token",
		"reflog-bearer",
		"reflog-standalone",
		"super-secret-signing-key",
		"sensitive@example.com",
		"Sensitive Name",
	} {
		if strings.Contains(raw, secret) {
			t.Fatalf("bundle leaked %q:\n%s", secret, raw)
		}
	}

	var result JSONDoctorBundleOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, raw)
	}
	if result.Redaction.SecretsIncluded {
		t.Fatal("SecretsIncluded = true, want false")
	}
	if result.Redaction.SourceIncluded {
		t.Fatal("SourceIncluded = true, want false")
	}
	if !result.UserConfig.Loaded || !result.UserConfig.TokenSet || !result.UserConfig.SigningKeyConfigured {
		t.Fatalf("unexpected user config summary: %+v", result.UserConfig)
	}
	if len(result.Repository.Remotes) != 1 {
		t.Fatalf("remotes = %+v, want one remote", result.Repository.Remotes)
	}
	if result.Protocol.SupportedRemoteProtocolVersion != remote.ProtocolVersion {
		t.Fatalf("protocol version = %q, want %q", result.Protocol.SupportedRemoteProtocolVersion, remote.ProtocolVersion)
	}
	if !stringSliceContains(result.Protocol.ClientCapabilities, remote.CapPack) {
		t.Fatalf("protocol client capabilities missing %q: %+v", remote.CapPack, result.Protocol.ClientCapabilities)
	}
	if !stringSliceContains(result.Protocol.ServerLimitKeys, "max_payload") {
		t.Fatalf("protocol server limit keys missing max_payload: %+v", result.Protocol.ServerLimitKeys)
	}
	if !doctorBundleProtocolHasResponseLimit(result.Protocol, "batchObjects") {
		t.Fatalf("protocol response limits missing batchObjects: %+v", result.Protocol.ResponseLimits)
	}
	if !doctorBundleProtocolHasCode(result.Protocol, "remote_transport_insecure") {
		t.Fatalf("protocol diagnostics missing remote_transport_insecure: %+v", result.Protocol.Diagnostics)
	}
	redactedRemote := result.Repository.Remotes[0].URL
	if !strings.Contains(redactedRemote, "redacted") {
		t.Fatalf("remote URL was not redacted: %q", redactedRemote)
	}
	if strings.Contains(redactedRemote, "remote-secret") || strings.Contains(redactedRemote, "remote-token") {
		t.Fatalf("redacted remote leaked secret: %q", redactedRemote)
	}
	if len(result.RecentReflog) != 1 {
		t.Fatalf("recent reflog entries = %+v, want one", result.RecentReflog)
	}
	if reason := result.RecentReflog[0].Reason; !strings.Contains(reason, "redacted@example.com") || !strings.Contains(reason, "Bearer redacted") {
		t.Fatalf("reflog reason was not redacted: %q", reason)
	}
}

func TestDoctorBundleReportsInsecureUserConfigPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "top-secret-token",
	}); err != nil {
		t.Fatalf("userconfig.Save: %v", err)
	}
	cfgPath := filepath.Join(home, ".graftconfig")
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	dir := t.TempDir()
	if _, err := repo.Init(dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--bundle"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONDoctorBundleOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.UserConfig.ConfigFileSecure {
		t.Fatalf("ConfigFileSecure = true, want false: %+v", result.UserConfig)
	}
	if result.UserConfig.ConfigFileMode != "-rw-r--r--" {
		t.Fatalf("ConfigFileMode = %q, want -rw-r--r--", result.UserConfig.ConfigFileMode)
	}
	if !strings.Contains(result.UserConfig.ConfigFileRepair, "chmod 600") {
		t.Fatalf("ConfigFileRepair = %q, want chmod repair", result.UserConfig.ConfigFileRepair)
	}
	if result.UserConfig.ConfigFileWarning == "" {
		t.Fatalf("ConfigFileWarning empty: %+v", result.UserConfig)
	}
}

func TestDoctorBundleReportsHookTrustWithoutHookContents(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := r.SetHooksTrusted(false); err != nil {
		t.Fatalf("SetHooksTrusted(false): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.toml"), []byte("[pre-commit.secret]\nrun = \"echo hook-toml-secret\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(hooks.toml): %v", err)
	}
	hooksDir := filepath.Join(r.GraftDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(hooks): %v", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte("#!/bin/sh\necho hook-script-secret\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(pre-commit): %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--bundle"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	for _, secret := range []string{"hook-toml-secret", "hook-script-secret"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("bundle leaked hook content %q:\n%s", secret, raw)
		}
	}

	var result JSONDoctorBundleOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, raw)
	}
	if result.Hooks.Trusted {
		t.Fatalf("Hooks.Trusted = true, want false: %+v", result.Hooks)
	}
	if !result.Hooks.Configured {
		t.Fatalf("Hooks.Configured = false, want true: %+v", result.Hooks)
	}
	if !result.Hooks.HooksTomlPresent {
		t.Fatalf("HooksTomlPresent = false, want true: %+v", result.Hooks)
	}
	if len(result.Hooks.ExecutableHooks) != 1 || result.Hooks.ExecutableHooks[0] != "pre-commit" {
		t.Fatalf("ExecutableHooks = %+v, want [pre-commit]", result.Hooks.ExecutableHooks)
	}
	if result.Hooks.Warning == "" {
		t.Fatalf("Hooks.Warning empty: %+v", result.Hooks)
	}
	if !strings.Contains(result.Hooks.Repair, "hooks.trusted true") {
		t.Fatalf("Hooks.Repair = %q, want trust repair command", result.Hooks.Repair)
	}
}

func TestDoctorCmdGlobalJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--global", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nraw: %s", err, out.String())
	}

	var result JSONDoctorGlobalOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if !result.OK {
		t.Fatalf("ok = false, want true: %+v", result.Diagnostics)
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if result.Version != version {
		t.Fatalf("version = %q, want %q", result.Version, version)
	}
	if !result.Git.Found || result.Git.Version == "" {
		t.Fatalf("git preflight missing: %+v", result.Git)
	}
	if result.UserConfig.ConfigFilePresent {
		t.Fatalf("ConfigFilePresent = true, want false: %+v", result.UserConfig)
	}
	if !globalDoctorOutputHasCode(result, "user_name_not_configured") {
		t.Fatalf("expected user_name_not_configured warning: %+v", result.Diagnostics)
	}
}

func TestDoctorCmdGlobalReportsInsecureUserConfigPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "top-secret-token",
	}); err != nil {
		t.Fatalf("userconfig.Save: %v", err)
	}
	cfgPath := filepath.Join(home, ".graftconfig")
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--global", "--json"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want insecure config failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}

	var result JSONDoctorGlobalOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK {
		t.Fatal("ok = true, want false")
	}
	if !globalDoctorOutputHasCode(result, "user_config_permissions_insecure") {
		t.Fatalf("diagnostics missing user_config_permissions_insecure: %+v", result.Diagnostics)
	}
	if strings.Contains(out.String(), "top-secret-token") {
		t.Fatalf("global doctor leaked token:\n%s", out.String())
	}
}

func TestDoctorCmdTextOutsideRepoFallsBackToGlobalPreflight(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nraw: %s", err, out.String())
	}

	raw := out.String()
	for _, want := range []string{
		"no graft repository found; running global install preflight",
		"ok: graft install preflight passed",
		"git:",
		"user config:",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("global fallback text missing %q\nraw:\n%s", want, raw)
		}
	}
}

func TestDoctorCmdGlobalCannotUseBundle(t *testing.T) {
	cmd := newDoctorCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--global", "--bundle"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want usage error")
	}
	if got := commandExitCode(err); got != exitUsageError {
		t.Fatalf("exit code = %d, want %d", got, exitUsageError)
	}
}

func doctorOutputHasCode(result JSONDoctorOutput, code string) bool {
	_, ok := doctorOutputDiagnostic(result, code)
	return ok
}

func doctorOutputDiagnostic(result JSONDoctorOutput, code string) (JSONRepositoryDiagnostic, bool) {
	for _, d := range result.Diagnostics {
		if d.Code == code {
			return d, true
		}
	}
	return JSONRepositoryDiagnostic{}, false
}

func globalDoctorOutputHasCode(result JSONDoctorGlobalOutput, code string) bool {
	for _, d := range result.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func doctorBundleProtocolHasResponseLimit(protocol JSONDoctorBundleProtocol, name string) bool {
	for _, limit := range protocol.ResponseLimits {
		if limit.Name == name && limit.Bytes > 0 {
			return true
		}
	}
	return false
}

func doctorBundleProtocolHasCode(protocol JSONDoctorBundleProtocol, code string) bool {
	for _, diagnostic := range protocol.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
