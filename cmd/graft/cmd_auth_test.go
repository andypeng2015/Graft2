package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

func TestResolveSSHKeyChoiceFromPath_PublicKeyFallback(t *testing.T) {
	dir := t.TempDir()
	keyBase := filepath.Join(dir, "id_ed25519")
	pubPath := keyBase + ".pub"

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	pubLine := string(ssh.MarshalAuthorizedKey(sshPub))
	if err := os.WriteFile(pubPath, []byte(pubLine), 0o644); err != nil {
		t.Fatalf("write pub: %v", err)
	}

	choice, err := resolveSSHKeyChoiceFromPath(keyBase, "")
	if err != nil {
		t.Fatalf("resolveSSHKeyChoiceFromPath: %v", err)
	}
	if choice.Path != pubPath {
		t.Fatalf("Path = %q, want %q", choice.Path, pubPath)
	}
	if choice.Name != "id_ed25519" {
		t.Fatalf("Name = %q, want id_ed25519", choice.Name)
	}
	if choice.PublicKey == "" {
		t.Fatalf("PublicKey is empty")
	}
	if choice.Fingerprint != ssh.FingerprintSHA256(sshPub) {
		t.Fatalf("Fingerprint mismatch")
	}
}

func TestDiscoverSSHPublicKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}

	pub1, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub1, err := ssh.NewPublicKey(pub1)
	if err != nil {
		t.Fatalf("NewPublicKey(1): %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "b_key.pub"), ssh.MarshalAuthorizedKey(sshPub1), 0o644); err != nil {
		t.Fatalf("write b_key.pub: %v", err)
	}

	pub2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub2, err := ssh.NewPublicKey(pub2)
	if err != nil {
		t.Fatalf("NewPublicKey(2): %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "a_key.pub"), ssh.MarshalAuthorizedKey(sshPub2), 0o644); err != nil {
		t.Fatalf("write a_key.pub: %v", err)
	}

	choices, err := discoverSSHPublicKeys()
	if err != nil {
		t.Fatalf("discoverSSHPublicKeys: %v", err)
	}
	if len(choices) != 2 {
		t.Fatalf("len(choices) = %d, want 2", len(choices))
	}
	if filepath.Base(choices[0].Path) != "a_key.pub" {
		t.Fatalf("choices[0] = %q, want a_key.pub", choices[0].Path)
	}
	if filepath.Base(choices[1].Path) != "b_key.pub" {
		t.Fatalf("choices[1] = %q, want b_key.pub", choices[1].Path)
	}
}

func TestResolveSSHKeyChoiceFromPath_PrivateKeyFallback(t *testing.T) {
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "id_ed25519")

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8,
	})
	if err := os.WriteFile(privatePath, pemData, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	choice, err := resolveSSHKeyChoiceFromPath(privatePath, "agent-key")
	if err != nil {
		t.Fatalf("resolveSSHKeyChoiceFromPath: %v", err)
	}
	if choice.Path != privatePath {
		t.Fatalf("Path = %q, want %q", choice.Path, privatePath)
	}
	if choice.Name != "agent-key" {
		t.Fatalf("Name = %q, want agent-key", choice.Name)
	}
	if choice.PublicKey == "" {
		t.Fatal("PublicKey is empty")
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(choice.PublicKey))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey(choice.PublicKey): %v", err)
	}
	if choice.Fingerprint != ssh.FingerprintSHA256(pub) {
		t.Fatalf("Fingerprint mismatch")
	}
}

func TestMintBootstrapToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/ssh/bootstrap/token" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["ttl_seconds"] != float64(180) {
			t.Fatalf("ttl_seconds = %v, want 180", req["ttl_seconds"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bootstrap_token":"minted-token","expires_at":"2026-02-25T12:00:00Z"}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	resp, err := mintBootstrapToken(cmd, server.URL, "test-token", 180)
	if err != nil {
		t.Fatalf("mintBootstrapToken: %v", err)
	}
	if strings.TrimSpace(resp.BootstrapToken) != "minted-token" {
		t.Fatalf("BootstrapToken = %q, want minted-token", resp.BootstrapToken)
	}
}

func TestMintBootstrapTokenMissingTokenInResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"expires_at":"2026-02-25T12:00:00Z"}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	_, err := mintBootstrapToken(cmd, server.URL, "test-token", 0)
	if err == nil {
		t.Fatal("expected error when bootstrap token missing in response")
	}
}

func TestWriteAuthConfigStoresHostProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := writeAuthConfig("https://orchard.example.com", "orchard-token", authUser{Username: "orchard-user"}); err != nil {
		t.Fatalf("writeAuthConfig orchard: %v", err)
	}
	if err := writeAuthConfig("https://code.example.com/api/v1", "code-token", authUser{Username: "code-user"}); err != nil {
		t.Fatalf("writeAuthConfig code: %v", err)
	}

	cfg, err := userconfig.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.DefaultOrchardURL(); got != "https://code.example.com/api/v1" {
		t.Fatalf("DefaultOrchardURL() = %q, want https://code.example.com/api/v1", got)
	}
	if cfg.Token != "code-token" {
		t.Fatalf("cfg.Token = %q, want code-token", cfg.Token)
	}
	if cfg.Username != "code-user" {
		t.Fatalf("cfg.Username = %q, want code-user", cfg.Username)
	}
	if cfg.Owner != "code-user" {
		t.Fatalf("cfg.Owner = %q, want code-user", cfg.Owner)
	}

	orchardProfile := cfg.OrchardProfile("https://orchard.example.com")
	if orchardProfile.Token != "orchard-token" {
		t.Fatalf("orchard profile token = %q, want orchard-token", orchardProfile.Token)
	}
	if orchardProfile.Username != "orchard-user" {
		t.Fatalf("orchard profile username = %q, want orchard-user", orchardProfile.Username)
	}

	codeProfile := cfg.OrchardProfile("https://code.example.com/api/v1")
	if codeProfile.Token != "code-token" {
		t.Fatalf("code profile token = %q, want code-token", codeProfile.Token)
	}
	if codeProfile.Username != "code-user" {
		t.Fatalf("code profile username = %q, want code-user", codeProfile.Username)
	}
}

func TestFormatAuthStatusLinesAllHosts(t *testing.T) {
	cfg := &userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "orchard-token",
		Username:   "orchard-user",
		Owner:      "orchard-owner",
		OrchardProfiles: map[string]userconfig.OrchardProfile{
			"https://code.example.com/api/v1": {
				Token:    "code-token",
				Username: "code-user",
				Owner:    "code-owner",
			},
		},
	}

	lines := formatAuthStatusLines(cfg, "/tmp/.graftconfig", "https://code.example.com/api/v1", true)
	out := strings.Join(lines, "\n")
	for _, want := range []string{
		"config: /tmp/.graftconfig",
		"host: https://code.example.com/api/v1 (selected, token:set)",
		"username: code-user",
		"owner: code-owner",
		"host: https://orchard.example.com (default, token:set)",
		"username: orchard-user",
		"owner: orchard-owner",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestAuthStatusWarnsOnInsecureConfigPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "secret-token",
		Username:   "alice",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfgPath := filepath.Join(home, ".graftconfig")
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	var out bytes.Buffer
	cmd := newAuthStatusCmd()
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw := out.String()
	if !strings.Contains(raw, "warning: user config") {
		t.Fatalf("status output missing permissions warning:\n%s", raw)
	}
	if !strings.Contains(raw, "repair: chmod 600") {
		t.Fatalf("status output missing chmod repair:\n%s", raw)
	}
}

func TestAuthDoctorJSONReportsEnvTokenWithoutLeakingSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFT_TOKEN", "env-secret-token")

	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "stored-secret-token",
		Username:   "alice",
		Owner:      "alice",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	cmd := newAuthDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--host", "https://orchard.example.com", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\nraw: %s", err, out.String())
	}

	raw := out.String()
	for _, secret := range []string{"env-secret-token", "stored-secret-token"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("auth doctor leaked %q:\n%s", secret, raw)
		}
	}

	var result JSONAuthDoctorOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, raw)
	}
	if !result.OK {
		t.Fatalf("ok = false, want true: %+v", result.Diagnostics)
	}
	if !result.TokenSet {
		t.Fatalf("TokenSet = false, want true: %+v", result)
	}
	if result.TokenSource != "env:GRAFT_TOKEN" {
		t.Fatalf("TokenSource = %q, want env:GRAFT_TOKEN", result.TokenSource)
	}
	if !authDoctorOutputHasCode(result, "auth_token_expiry_unknown") {
		t.Fatalf("expected opaque-token expiry warning: %+v", result.Diagnostics)
	}
}

func TestAuthDoctorJSONReportsExpiredJWT(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFT_TOKEN", "")

	expiredToken := unsignedTestJWT(time.Now().Add(-time.Hour))
	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      expiredToken,
		Username:   "alice",
		Owner:      "alice",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	cmd := newAuthDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--host", "https://orchard.example.com", "--json"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want expired token failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}
	if strings.Contains(out.String(), expiredToken) {
		t.Fatalf("auth doctor leaked token:\n%s", out.String())
	}

	var result JSONAuthDoctorOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK {
		t.Fatal("ok = true, want false")
	}
	if !result.TokenExpiryKnown || !result.TokenExpired {
		t.Fatalf("token expiry fields = known:%t expired:%t, want true/true", result.TokenExpiryKnown, result.TokenExpired)
	}
	if !authDoctorOutputHasCode(result, "auth_token_expired") {
		t.Fatalf("diagnostics missing auth_token_expired: %+v", result.Diagnostics)
	}
}

func TestAuthDoctorJSONReportsInsecureConfigPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFT_TOKEN", "")

	if err := userconfig.Save(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "stored-secret-token",
		Username:   "alice",
		Owner:      "alice",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfgPath := filepath.Join(home, ".graftconfig")
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	var out bytes.Buffer
	cmd := newAuthDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--host", "https://orchard.example.com", "--json"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want insecure config failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}
	if strings.Contains(out.String(), "stored-secret-token") {
		t.Fatalf("auth doctor leaked token:\n%s", out.String())
	}

	var result JSONAuthDoctorOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK {
		t.Fatal("ok = true, want false")
	}
	if !authDoctorOutputHasCode(result, "user_config_permissions_insecure") {
		t.Fatalf("diagnostics missing user_config_permissions_insecure: %+v", result.Diagnostics)
	}
}

func TestAuthDoctorJSONReportsMissingToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAFT_TOKEN", "")

	var out bytes.Buffer
	cmd := newAuthDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--host", "https://orchard.example.com", "--json"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want missing token failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}

	var result JSONAuthDoctorOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK {
		t.Fatal("ok = true, want false")
	}
	if !authDoctorOutputHasCode(result, "auth_token_missing") {
		t.Fatalf("diagnostics missing auth_token_missing: %+v", result.Diagnostics)
	}
}

func TestClearAllStoredAuthTokensPreservesProfiles(t *testing.T) {
	cfg := &userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "orchard-token",
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
	}

	clearAllStoredAuthTokens(cfg)

	if cfg.Token != "" {
		t.Fatalf("cfg.Token = %q, want empty", cfg.Token)
	}
	for host, wantUser := range map[string]string{
		"https://orchard.example.com":     "orchard-user",
		"https://code.example.com/api/v1": "code-user",
	} {
		profile := cfg.OrchardProfile(host)
		if profile.Token != "" {
			t.Fatalf("%s token = %q, want empty", host, profile.Token)
		}
		if profile.Username != wantUser {
			t.Fatalf("%s username = %q, want %q", host, profile.Username, wantUser)
		}
	}
}

func authDoctorOutputHasCode(result JSONAuthDoctorOutput, code string) bool {
	for _, d := range result.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func unsignedTestJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return header + "." + payload + "."
}
