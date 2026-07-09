package main

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestTagCmdSignedTagVerifyJSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeVerifyCmdFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	keyPath := filepath.Join(dir, "id_ed25519")
	if err := repo.GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}
	allowedSignersPath := filepath.Join(dir, "allowed_signers")
	writeAllowedSignerForKey(t, allowedSignersPath, "release@example.com", keyPath)

	restore := chdirForTest(t, dir)
	defer restore()

	createCmd := newTagCmd()
	createCmd.SilenceUsage = true
	createCmd.SetOut(io.Discard)
	createCmd.SetErr(io.Discard)
	createCmd.SetArgs([]string{"--annotate", "--message", "release 1.0.0", "--sign-key", keyPath, "v1.0.0"})
	if err := createCmd.Execute(); err != nil {
		t.Fatalf("create Execute: %v", err)
	}

	var out bytes.Buffer
	verifyCmd := newTagCmd()
	verifyCmd.SilenceUsage = true
	verifyCmd.SetOut(&out)
	verifyCmd.SetErr(io.Discard)
	verifyCmd.SetArgs([]string{"--verify", "v1.0.0", "--allowed-signers", allowedSignersPath, "--json"})
	if err := verifyCmd.Execute(); err != nil {
		t.Fatalf("verify Execute: %v", err)
	}

	var result JSONTagVerifyOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if !result.OK || !result.Valid || result.Unsigned || !result.AllowedSigners {
		t.Fatalf("unexpected tag verification result: %+v", result)
	}
	if result.SignerKey != "release@example.com" || result.Algorithm != "ssh-ed25519" {
		t.Fatalf("unexpected signer metadata: %+v", result)
	}
}

func TestTagCmdVerifyRequireSignedFailsLightweight(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeVerifyCmdFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	head, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := r.CreateTag("v1.0.0", head, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newTagCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--verify", "v1.0.0", "--require-signed", "--json"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want verification failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d; err=%v", got, exitVerificationFailure, err)
	}

	var result JSONTagVerifyOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK || !result.Unsigned || !result.RequireSigned {
		t.Fatalf("unexpected tag verification result: %+v", result)
	}
}

func TestTagCmdVerifyRejectsUntrustedSigner(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeVerifyCmdFile(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	head, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	signingKeyPath := filepath.Join(dir, "signing")
	otherKeyPath := filepath.Join(dir, "other")
	if err := repo.GenerateSigningKey(signingKeyPath); err != nil {
		t.Fatalf("GenerateSigningKey signing: %v", err)
	}
	if err := repo.GenerateSigningKey(otherKeyPath); err != nil {
		t.Fatalf("GenerateSigningKey other: %v", err)
	}
	signer, err := repo.NewSSHSigner(signingKeyPath)
	if err != nil {
		t.Fatalf("NewSSHSigner: %v", err)
	}
	if _, err := r.CreateAnnotatedTagWithSigner("v1.0.0", head, "tester", "release", false, signer); err != nil {
		t.Fatalf("CreateAnnotatedTagWithSigner: %v", err)
	}
	allowedSignersPath := filepath.Join(dir, "allowed_signers")
	writeAllowedSignerForKey(t, allowedSignersPath, "other@example.com", otherKeyPath)

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newTagCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--verify", "v1.0.0", "--allowed-signers", allowedSignersPath, "--json"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want verification failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d; err=%v", got, exitVerificationFailure, err)
	}

	var result JSONTagVerifyOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK || result.Valid || !result.AllowedSigners {
		t.Fatalf("unexpected tag verification result: %+v", result)
	}
	if !strings.Contains(result.Error, "not in allowed signers") {
		t.Fatalf("error = %q, want allowed signers failure", result.Error)
	}
}
