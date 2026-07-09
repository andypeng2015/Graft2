package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestReleaseManifestCmdJSONFilesAndDirs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "b.txt"), []byte("bravo\n"))
	writeTestFile(t, filepath.Join(dir, "sub", "a.txt"), []byte("alpha\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newReleaseManifestCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "sub", "b.txt"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONReleaseManifestOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if result.Version != version || result.GoVersion == "" || result.GeneratedAt == "" {
		t.Fatalf("missing manifest metadata: %+v", result)
	}
	if len(result.Files) != 2 {
		t.Fatalf("files len = %d, want 2: %+v", len(result.Files), result.Files)
	}
	if result.Files[0].Path != "b.txt" || result.Files[1].Path != "sub/a.txt" {
		t.Fatalf("files sorted paths = %+v, want b.txt then sub/a.txt", result.Files)
	}
	assertManifestFile(t, result.Files[0], "b.txt", []byte("bravo\n"))
	assertManifestFile(t, result.Files[1], "sub/a.txt", []byte("alpha\n"))
}

func TestReleaseManifestCmdText(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("artifact\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newReleaseManifestCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"artifact.txt"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	raw := out.String()
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte("artifact\n")))
	for _, want := range []string{wantHash, "9", "artifact.txt"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("manifest text missing %q:\n%s", want, raw)
		}
	}
}

func TestReleaseManifestCmdMissingFileFails(t *testing.T) {
	var out bytes.Buffer
	cmd := newReleaseManifestCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"missing-artifact"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute succeeded, want missing artifact error")
	}
}

func TestReleaseVerifyManifestCmdJSONSuccess(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("artifact\n"))
	manifestPath := writeReleaseManifestForTest(t, dir, []JSONReleaseManifestFile{
		releaseManifestFileForTest(t, "artifact.txt", []byte("artifact\n")),
	})

	var out bytes.Buffer
	cmd := newReleaseVerifyManifestCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "--base-dir", dir, manifestPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONReleaseManifestVerificationOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if !result.OK || result.Checked != 1 || result.Matched != 1 || result.ManifestFormat != "json" {
		t.Fatalf("unexpected verification result: %+v", result)
	}
	if len(result.Results) != 1 || !result.Results[0].OK || result.Results[0].Status != "matched" {
		t.Fatalf("unexpected file results: %+v", result.Results)
	}
}

func TestReleaseVerifyManifestCmdMismatchFailsWithJSON(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("current\n"))
	manifestPath := writeReleaseManifestForTest(t, dir, []JSONReleaseManifestFile{
		releaseManifestFileForTest(t, "artifact.txt", []byte("stale!!\n")),
	})

	var out bytes.Buffer
	cmd := newReleaseVerifyManifestCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "--base-dir", dir, manifestPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want verification failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}

	var result JSONReleaseManifestVerificationOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK || result.Mismatched != 1 {
		t.Fatalf("unexpected mismatch summary: %+v", result)
	}
	if len(result.Results) != 1 || result.Results[0].Status != "hash_mismatch" {
		t.Fatalf("unexpected file results: %+v", result.Results)
	}
}

func TestReleaseVerifyManifestCmdMissingFileFails(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeReleaseManifestForTest(t, dir, []JSONReleaseManifestFile{
		releaseManifestFileForTest(t, "missing.txt", []byte("missing\n")),
	})

	var out bytes.Buffer
	cmd := newReleaseVerifyManifestCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "--base-dir", dir, manifestPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want verification failure")
	}

	var result JSONReleaseManifestVerificationOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK || result.Missing != 1 || result.Results[0].Status != "missing" {
		t.Fatalf("unexpected missing-file result: %+v", result)
	}
}

func TestReleaseVerifyManifestCmdRejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeReleaseManifestForTest(t, dir, []JSONReleaseManifestFile{
		releaseManifestFileForTest(t, "../secret.txt", []byte("secret\n")),
	})

	var out bytes.Buffer
	cmd := newReleaseVerifyManifestCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "--base-dir", dir, manifestPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want verification failure")
	}

	var result JSONReleaseManifestVerificationOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK || result.Errors != 1 || result.Results[0].Status != "invalid_path" {
		t.Fatalf("unexpected unsafe-path result: %+v", result)
	}
}

func TestReleaseVerifyManifestCmdTextManifestSuccess(t *testing.T) {
	dir := t.TempDir()
	content := []byte("artifact\n")
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), content)
	entry := releaseManifestFileForTest(t, "artifact.txt", content)
	manifestPath := filepath.Join(dir, "manifest.txt")
	writeTestFile(t, manifestPath, []byte(fmt.Sprintf("%s  %d  %s\n", entry.SHA256, entry.SizeBytes, entry.Path)))

	var out bytes.Buffer
	cmd := newReleaseVerifyManifestCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--base-dir", dir, manifestPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "ok: verified 1 release artifact(s)") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestReleaseCheckCmdJSONSuccess(t *testing.T) {
	dir := t.TempDir()
	changelogPath := filepath.Join(dir, "CHANGELOG.md")
	writeTestFile(t, changelogPath, []byte("# Changelog\n\n## v1.2.3\n\n- release note\n"))

	var out bytes.Buffer
	cmd := newReleaseCheckCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "--version", "1.2.3", "--changelog", changelogPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONReleaseCheckOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.SchemaVersion != JSONSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", result.SchemaVersion, JSONSchemaVersion)
	}
	if !result.OK || result.Version != "v1.2.3" || len(result.Checks) != 3 {
		t.Fatalf("unexpected release check result: %+v", result)
	}
}

func TestReleaseCheckCmdMissingChangelogEntryFailsWithJSON(t *testing.T) {
	dir := t.TempDir()
	changelogPath := filepath.Join(dir, "CHANGELOG.md")
	writeTestFile(t, changelogPath, []byte("# Changelog\n\n## v1.2.2\n\n- old release note\n"))

	var out bytes.Buffer
	cmd := newReleaseCheckCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "--version", "1.2.3", "--changelog", changelogPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want release check failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}

	var result JSONReleaseCheckOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.OK || !releaseCheckHasFailedCheck(result, "changelog-entry") {
		t.Fatalf("unexpected missing-entry result: %+v", result)
	}
}

func TestReleaseCheckCmdTextSuccess(t *testing.T) {
	dir := t.TempDir()
	changelogPath := filepath.Join(dir, "CHANGELOG.md")
	writeTestFile(t, changelogPath, []byte("# Changelog\n\n## v1.2.3\n\n- release note\n"))

	var out bytes.Buffer
	cmd := newReleaseCheckCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--version", "v1.2.3", "--changelog", changelogPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "ok: release checks passed for v1.2.3") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestReleaseSBOMCmdGeneratesSPDXJSON(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("artifact\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newReleaseSBOMCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--name", "graft-test", "artifact.txt"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result releaseSPDXDocument
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.SPDXVersion != "SPDX-2.3" || result.DataLicense != "CC0-1.0" || result.Name != "graft-test" {
		t.Fatalf("unexpected SPDX document metadata: %+v", result)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files len = %d, want 1: %+v", len(result.Files), result.Files)
	}
	file := result.Files[0]
	if file.FileName != "artifact.txt" || file.LicenseConcluded != "NOASSERTION" {
		t.Fatalf("unexpected SPDX file: %+v", file)
	}
	if len(file.Checksums) != 1 || file.Checksums[0].Algorithm != "SHA256" {
		t.Fatalf("unexpected checksums: %+v", file.Checksums)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte("artifact\n")))
	if file.Checksums[0].ChecksumValue != wantHash {
		t.Fatalf("checksum = %q, want %q", file.Checksums[0].ChecksumValue, wantHash)
	}
	if len(result.Relationships) != 1 || result.Relationships[0].RelatedSPDXElement != file.SPDXID {
		t.Fatalf("unexpected relationships: %+v", result.Relationships)
	}
}

func TestReleaseProvenanceCmdGeneratesStatement(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("artifact\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newReleaseProvenanceCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"--builder-id", "https://builder.example/graft",
		"--build-type", "https://build.example/release",
		"artifact.txt",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result releaseProvenanceStatement
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if result.Type != "https://in-toto.io/Statement/v1" || result.PredicateType != "https://slsa.dev/provenance/v1" {
		t.Fatalf("unexpected statement metadata: %+v", result)
	}
	if result.Predicate.BuildDefinition.BuildType != "https://build.example/release" {
		t.Fatalf("buildType = %q", result.Predicate.BuildDefinition.BuildType)
	}
	if result.Predicate.RunDetails.Builder.ID != "https://builder.example/graft" {
		t.Fatalf("builder id = %q", result.Predicate.RunDetails.Builder.ID)
	}
	if result.Predicate.RunDetails.Metadata.InvocationID == "" {
		t.Fatal("missing invocation id")
	}
	if len(result.Subject) != 1 || result.Subject[0].Name != "artifact.txt" {
		t.Fatalf("unexpected subjects: %+v", result.Subject)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte("artifact\n")))
	if result.Subject[0].Digest["sha256"] != wantHash {
		t.Fatalf("subject sha256 = %q, want %q", result.Subject[0].Digest["sha256"], wantHash)
	}
}

func TestReleaseSignAndVerifySignatureCmd(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := repo.GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}
	allowedSignersPath := filepath.Join(dir, "allowed_signers")
	writeAllowedSignerForKey(t, allowedSignersPath, "release@example.com", keyPath)
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("artifact\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var signOut bytes.Buffer
	signCmd := newReleaseSignCmd()
	signCmd.SilenceUsage = true
	signCmd.SetOut(&signOut)
	signCmd.SetErr(io.Discard)
	signCmd.SetArgs([]string{"--sign-key", keyPath, "artifact.txt"})

	if err := signCmd.Execute(); err != nil {
		t.Fatalf("sign Execute: %v", err)
	}

	var signed JSONReleaseSignOutput
	if err := json.Unmarshal(signOut.Bytes(), &signed); err != nil {
		t.Fatalf("signature output is not valid JSON: %v\nraw: %s", err, signOut.String())
	}
	if signed.SchemaVersion != JSONSchemaVersion || signed.SignatureFormat != "sshsig-v1" || signed.PayloadFormat != "graft-release-artifact-v1" {
		t.Fatalf("unexpected signature output metadata: %+v", signed)
	}
	if len(signed.Files) != 1 || signed.Files[0].Signature == "" {
		t.Fatalf("unexpected signature files: %+v", signed.Files)
	}

	signaturePath := filepath.Join(dir, "signatures.json")
	writeTestFile(t, signaturePath, signOut.Bytes())

	var verifyOut bytes.Buffer
	verifyCmd := newReleaseVerifySignatureCmd()
	verifyCmd.SilenceUsage = true
	verifyCmd.SetOut(&verifyOut)
	verifyCmd.SetErr(io.Discard)
	verifyCmd.SetArgs([]string{"--json", "--base-dir", dir, "--allowed-signers", allowedSignersPath, signaturePath})

	if err := verifyCmd.Execute(); err != nil {
		t.Fatalf("verify Execute: %v", err)
	}

	var result JSONReleaseVerifySignatureOutput
	if err := json.Unmarshal(verifyOut.Bytes(), &result); err != nil {
		t.Fatalf("verify output is not valid JSON: %v\nraw: %s", err, verifyOut.String())
	}
	if !result.OK || result.Valid != 1 || result.Results[0].SignerKey == "" || result.Results[0].Algorithm == "" {
		t.Fatalf("unexpected verify result: %+v", result)
	}
	if result.Results[0].SignerKey != "release@example.com" {
		t.Fatalf("signer key = %q, want allowed signer name", result.Results[0].SignerKey)
	}
}

func TestReleaseVerifySignatureDetectsTamper(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := repo.GenerateSigningKey(keyPath); err != nil {
		t.Fatalf("GenerateSigningKey: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("artifact\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var signOut bytes.Buffer
	signCmd := newReleaseSignCmd()
	signCmd.SilenceUsage = true
	signCmd.SetOut(&signOut)
	signCmd.SetErr(io.Discard)
	signCmd.SetArgs([]string{"--sign-key", keyPath, "artifact.txt"})
	if err := signCmd.Execute(); err != nil {
		t.Fatalf("sign Execute: %v", err)
	}
	signaturePath := filepath.Join(dir, "signatures.json")
	writeTestFile(t, signaturePath, signOut.Bytes())
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("tampered\n"))

	var verifyOut bytes.Buffer
	verifyCmd := newReleaseVerifySignatureCmd()
	verifyCmd.SilenceUsage = true
	verifyCmd.SetOut(&verifyOut)
	verifyCmd.SetErr(io.Discard)
	verifyCmd.SetArgs([]string{"--json", "--base-dir", dir, signaturePath})

	err := verifyCmd.Execute()
	if err == nil {
		t.Fatal("verify succeeded, want tamper failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}
	var result JSONReleaseVerifySignatureOutput
	if err := json.Unmarshal(verifyOut.Bytes(), &result); err != nil {
		t.Fatalf("verify output is not valid JSON: %v\nraw: %s", err, verifyOut.String())
	}
	if result.OK || result.Mismatched != 1 || result.Results[0].Status != "hash_mismatch" {
		t.Fatalf("unexpected tamper result: %+v", result)
	}
}

func TestReleaseVerifySignatureRejectsUntrustedSigner(t *testing.T) {
	dir := t.TempDir()
	signingKeyPath := filepath.Join(dir, "id_ed25519")
	otherKeyPath := filepath.Join(dir, "other_ed25519")
	if err := repo.GenerateSigningKey(signingKeyPath); err != nil {
		t.Fatalf("GenerateSigningKey signing: %v", err)
	}
	if err := repo.GenerateSigningKey(otherKeyPath); err != nil {
		t.Fatalf("GenerateSigningKey other: %v", err)
	}
	allowedSignersPath := filepath.Join(dir, "allowed_signers")
	writeAllowedSignerForKey(t, allowedSignersPath, "other@example.com", otherKeyPath)
	writeTestFile(t, filepath.Join(dir, "artifact.txt"), []byte("artifact\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	var signOut bytes.Buffer
	signCmd := newReleaseSignCmd()
	signCmd.SilenceUsage = true
	signCmd.SetOut(&signOut)
	signCmd.SetErr(io.Discard)
	signCmd.SetArgs([]string{"--sign-key", signingKeyPath, "artifact.txt"})
	if err := signCmd.Execute(); err != nil {
		t.Fatalf("sign Execute: %v", err)
	}
	signaturePath := filepath.Join(dir, "signatures.json")
	writeTestFile(t, signaturePath, signOut.Bytes())

	var verifyOut bytes.Buffer
	verifyCmd := newReleaseVerifySignatureCmd()
	verifyCmd.SilenceUsage = true
	verifyCmd.SetOut(&verifyOut)
	verifyCmd.SetErr(io.Discard)
	verifyCmd.SetArgs([]string{"--json", "--base-dir", dir, "--allowed-signers", allowedSignersPath, signaturePath})

	err := verifyCmd.Execute()
	if err == nil {
		t.Fatal("verify succeeded, want untrusted signer failure")
	}
	if got := commandExitCode(err); got != exitVerificationFailure {
		t.Fatalf("exit code = %d, want %d", got, exitVerificationFailure)
	}
	var result JSONReleaseVerifySignatureOutput
	if err := json.Unmarshal(verifyOut.Bytes(), &result); err != nil {
		t.Fatalf("verify output is not valid JSON: %v\nraw: %s", err, verifyOut.String())
	}
	if result.OK || result.Invalid != 1 || result.Results[0].Status != "signature_untrusted" {
		t.Fatalf("unexpected untrusted signer result: %+v", result)
	}
}

func assertManifestFile(t *testing.T, got JSONReleaseManifestFile, path string, content []byte) {
	t.Helper()
	wantHash := fmt.Sprintf("%x", sha256.Sum256(content))
	if got.Path != path {
		t.Fatalf("path = %q, want %q", got.Path, path)
	}
	if got.SizeBytes != int64(len(content)) {
		t.Fatalf("%s size = %d, want %d", path, got.SizeBytes, len(content))
	}
	if got.SHA256 != wantHash {
		t.Fatalf("%s sha256 = %q, want %q", path, got.SHA256, wantHash)
	}
}

func TestReleaseCommandRegisteredOnRoot(t *testing.T) {
	cmd := newRootCmd()
	for _, child := range cmd.Commands() {
		if child.Name() == "release" {
			return
		}
	}
	t.Fatal("root command missing release subcommand")
}

func TestReleaseCommandRegistersVerifyManifestSubcommand(t *testing.T) {
	cmd := newReleaseCmd()
	for _, child := range cmd.Commands() {
		if child.Name() == "verify-manifest" {
			return
		}
	}
	t.Fatal("release command missing verify-manifest subcommand")
}

func TestReleaseCommandRegistersCheckSubcommand(t *testing.T) {
	cmd := newReleaseCmd()
	for _, child := range cmd.Commands() {
		if child.Name() == "check" {
			return
		}
	}
	t.Fatal("release command missing check subcommand")
}

func TestReleaseCommandRegistersSBOMSubcommand(t *testing.T) {
	cmd := newReleaseCmd()
	for _, child := range cmd.Commands() {
		if child.Name() == "sbom" {
			return
		}
	}
	t.Fatal("release command missing sbom subcommand")
}

func TestReleaseCommandRegistersProvenanceSubcommand(t *testing.T) {
	cmd := newReleaseCmd()
	for _, child := range cmd.Commands() {
		if child.Name() == "provenance" {
			return
		}
	}
	t.Fatal("release command missing provenance subcommand")
}

func TestReleaseCommandRegistersSignSubcommands(t *testing.T) {
	cmd := newReleaseCmd()
	names := map[string]bool{}
	for _, child := range cmd.Commands() {
		names[child.Name()] = true
	}
	for _, want := range []string{"sign", "verify-signature"} {
		if !names[want] {
			t.Fatalf("release command missing %s subcommand", want)
		}
	}
}

func TestCollectReleaseManifestAcceptsEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	if _, err := collectReleaseManifestFiles([]string{dir}); err != nil {
		t.Fatalf("empty directory should be accepted: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, err := collectReleaseManifestFiles([]string{filepath.Join(dir, "nested")}); err != nil {
		t.Fatalf("directory with no regular files should be accepted: %v", err)
	}
}

func writeReleaseManifestForTest(t *testing.T, dir string, files []JSONReleaseManifestFile) string {
	t.Helper()
	var buf bytes.Buffer
	if err := writeJSON(&buf, JSONReleaseManifestOutput{Files: files}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	writeTestFile(t, manifestPath, buf.Bytes())
	return manifestPath
}

func releaseManifestFileForTest(t *testing.T, path string, content []byte) JSONReleaseManifestFile {
	t.Helper()
	return JSONReleaseManifestFile{
		Path:      path,
		SizeBytes: int64(len(content)),
		SHA256:    fmt.Sprintf("%x", sha256.Sum256(content)),
	}
}

func releaseCheckHasFailedCheck(result JSONReleaseCheckOutput, name string) bool {
	for _, check := range result.Checks {
		if check.Name == name && !check.OK {
			return true
		}
	}
	return false
}

func writeAllowedSignerForKey(t *testing.T, path, name, keyPath string) {
	t.Helper()
	pubKey, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("ReadFile public key: %v", err)
	}
	writeTestFile(t, path, []byte(name+" "+strings.TrimSpace(string(pubKey))+"\n"))
}
