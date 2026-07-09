package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

func newReleaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Build release support artifacts",
	}
	cmd.AddCommand(newReleaseManifestCmd())
	cmd.AddCommand(newReleaseVerifyManifestCmd())
	cmd.AddCommand(newReleaseCheckCmd())
	cmd.AddCommand(newReleaseSBOMCmd())
	cmd.AddCommand(newReleaseProvenanceCmd())
	cmd.AddCommand(newReleaseSignCmd())
	cmd.AddCommand(newReleaseVerifySignatureCmd())
	return cmd
}

func newReleaseManifestCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "manifest <file-or-dir>...",
		Short: "Generate a checksum manifest for release artifacts",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := collectReleaseManifestFiles(args)
			if err != nil {
				return err
			}
			out := JSONReleaseManifestOutput{
				GeneratedAt:                    time.Now().UTC().Format(time.RFC3339),
				Version:                        version,
				Commit:                         commit,
				BuildTime:                      buildTime,
				GoVersion:                      runtime.Version(),
				SupportedRepositoryFormat:      repo.RepositoryFormatVersion,
				SupportedRemoteProtocolVersion: remote.ProtocolVersion,
				Files:                          files,
			}
			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), out)
			}
			for _, file := range files {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %d  %s\n", file.SHA256, file.SizeBytes, file.Path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	return cmd
}

func collectReleaseManifestFiles(paths []string) ([]JSONReleaseManifestFile, error) {
	var files []JSONReleaseManifestFile
	for _, input := range paths {
		path := filepath.Clean(input)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("manifest stat %q: %w", input, err)
		}
		if info.IsDir() {
			if err := filepath.WalkDir(path, func(p string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				manifestFile, err := releaseManifestFile(p)
				if err != nil {
					return err
				}
				files = append(files, manifestFile)
				return nil
			}); err != nil {
				return nil, fmt.Errorf("manifest walk %q: %w", input, err)
			}
			continue
		}
		manifestFile, err := releaseManifestFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, manifestFile)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func releaseManifestFile(path string) (JSONReleaseManifestFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return JSONReleaseManifestFile{}, fmt.Errorf("manifest stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return JSONReleaseManifestFile{}, fmt.Errorf("manifest %q: not a regular file", path)
	}

	sum, err := hashReleaseFile(path)
	if err != nil {
		return JSONReleaseManifestFile{}, fmt.Errorf("manifest hash %q: %w", path, err)
	}
	return JSONReleaseManifestFile{
		Path:      filepath.ToSlash(filepath.Clean(path)),
		SizeBytes: info.Size(),
		SHA256:    sum,
	}, nil
}

func newReleaseVerifyManifestCmd() *cobra.Command {
	var jsonFlag bool
	var baseDir string

	cmd := &cobra.Command{
		Use:   "verify-manifest [--base-dir <dir>] <manifest>",
		Short: "Verify release artifacts against a checksum manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manifestPath := args[0]
			files, format, err := loadReleaseManifest(manifestPath)
			if err != nil {
				return err
			}
			if strings.TrimSpace(baseDir) == "" {
				baseDir = "."
			}
			baseDir = filepath.Clean(baseDir)
			if info, err := os.Stat(baseDir); err != nil {
				return fmt.Errorf("verify manifest base-dir %q: %w", baseDir, err)
			} else if !info.IsDir() {
				return fmt.Errorf("verify manifest base-dir %q: not a directory", baseDir)
			}

			report := verifyReleaseManifestFiles(manifestPath, format, baseDir, files)
			if jsonFlag {
				if err := writeJSON(cmd.OutOrStdout(), report); err != nil {
					return err
				}
				if !report.OK {
					return verificationFailureError(releaseManifestVerificationError(report))
				}
				return nil
			}

			if report.OK {
				fmt.Fprintf(cmd.OutOrStdout(), "ok: verified %d release artifact(s)\n", report.Checked)
				return nil
			}
			printReleaseManifestVerificationReport(cmd.OutOrStdout(), report)
			return verificationFailureError(releaseManifestVerificationError(report))
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().StringVar(&baseDir, "base-dir", ".", "directory containing artifacts referenced by the manifest")
	return cmd
}

func newReleaseCheckCmd() *cobra.Command {
	var jsonFlag bool
	var releaseVersion string
	var changelogPath string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Run release preflight checks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(releaseVersion) == "" {
				releaseVersion = version
			}
			report := runReleaseChecks(releaseVersion, changelogPath)
			if jsonFlag {
				if err := writeJSON(cmd.OutOrStdout(), report); err != nil {
					return err
				}
				if !report.OK {
					return verificationFailureError(releaseCheckError(report))
				}
				return nil
			}
			if report.OK {
				fmt.Fprintf(cmd.OutOrStdout(), "ok: release checks passed for %s\n", report.Version)
				return nil
			}
			for _, check := range report.Checks {
				if !check.OK {
					fmt.Fprintf(cmd.OutOrStdout(), "failed: %s: %s\n", check.Name, check.Message)
				}
			}
			return verificationFailureError(releaseCheckError(report))
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().StringVar(&releaseVersion, "version", "", "release version to check (defaults to graft build version)")
	cmd.Flags().StringVar(&changelogPath, "changelog", "CHANGELOG.md", "path to changelog file")
	return cmd
}

func newReleaseSBOMCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "sbom <file-or-dir>...",
		Short: "Generate an SPDX JSON SBOM for release artifacts",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := collectReleaseManifestFiles(args)
			if err != nil {
				return err
			}
			doc := buildReleaseSBOM(name, files)
			return writeJSON(cmd.OutOrStdout(), doc)
		},
	}
	cmd.Flags().StringVar(&name, "name", "graft-release", "SPDX document name")
	return cmd
}

func newReleaseProvenanceCmd() *cobra.Command {
	var builderID string
	var buildType string

	cmd := &cobra.Command{
		Use:   "provenance <file-or-dir>...",
		Short: "Generate an in-toto/SLSA provenance statement for release artifacts",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := collectReleaseManifestFiles(args)
			if err != nil {
				return err
			}
			statement := buildReleaseProvenance(builderID, buildType, files)
			return writeJSON(cmd.OutOrStdout(), statement)
		},
	}
	cmd.Flags().StringVar(&builderID, "builder-id", "https://github.com/odvcencio/graft", "SLSA builder id")
	cmd.Flags().StringVar(&buildType, "build-type", "https://github.com/odvcencio/graft/release/v1", "SLSA build type")
	return cmd
}

func newReleaseSignCmd() *cobra.Command {
	var signKey string

	cmd := &cobra.Command{
		Use:   "sign [--sign-key <path>] <file-or-dir>...",
		Short: "Sign release artifact metadata with an SSH key",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := collectReleaseManifestFiles(args)
			if err != nil {
				return err
			}
			signer, _, err := newSSHCommitSigner(signKey)
			if err != nil {
				return err
			}
			out, err := signReleaseFiles(files, signer)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&signKey, "sign-key", "", "path to SSH private key (defaults to ~/.ssh/id_ed25519, id_ecdsa, id_rsa)")
	return cmd
}

func newReleaseVerifySignatureCmd() *cobra.Command {
	var jsonFlag bool
	var baseDir string
	var allowedSignersPath string

	cmd := &cobra.Command{
		Use:   "verify-signature [--base-dir <dir>] [--allowed-signers <file>] <signature-json>",
		Short: "Verify signed release artifact metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			signaturePath := args[0]
			signatures, err := loadReleaseSignatures(signaturePath)
			if err != nil {
				return err
			}
			if strings.TrimSpace(baseDir) == "" {
				baseDir = "."
			}
			baseDir = filepath.Clean(baseDir)
			if info, err := os.Stat(baseDir); err != nil {
				return fmt.Errorf("verify release signature base-dir %q: %w", baseDir, err)
			} else if !info.IsDir() {
				return fmt.Errorf("verify release signature base-dir %q: not a directory", baseDir)
			}
			allowedSigners, err := loadReleaseAllowedSigners(allowedSignersPath)
			if err != nil {
				return err
			}
			report := verifyReleaseSignatures(signaturePath, baseDir, signatures.Files, allowedSigners)
			if jsonFlag {
				if err := writeJSON(cmd.OutOrStdout(), report); err != nil {
					return err
				}
				if !report.OK {
					return verificationFailureError(releaseSignatureVerificationError(report))
				}
				return nil
			}
			if report.OK {
				fmt.Fprintf(cmd.OutOrStdout(), "ok: verified %d release signature(s)\n", report.Checked)
				return nil
			}
			printReleaseSignatureVerificationReport(cmd.OutOrStdout(), report)
			return verificationFailureError(releaseSignatureVerificationError(report))
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().StringVar(&baseDir, "base-dir", ".", "directory containing artifacts referenced by the signature file")
	cmd.Flags().StringVar(&allowedSignersPath, "allowed-signers", "", "OpenSSH allowed_signers file for trusted release keys")
	return cmd
}

func hashReleaseFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func loadReleaseManifest(path string) ([]JSONReleaseManifestFile, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read release manifest %q: %w", path, err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, "", fmt.Errorf("read release manifest %q: empty manifest", path)
	}
	if trimmed[0] == '{' {
		var manifest JSONReleaseManifestOutput
		if err := json.Unmarshal(trimmed, &manifest); err != nil {
			return nil, "", fmt.Errorf("parse release manifest %q as JSON: %w", path, err)
		}
		if manifest.SchemaVersion != 0 && manifest.SchemaVersion != JSONSchemaVersion {
			return nil, "", fmt.Errorf("parse release manifest %q: unsupported schemaVersion %d", path, manifest.SchemaVersion)
		}
		if err := validateReleaseManifestFiles(manifest.Files); err != nil {
			return nil, "", fmt.Errorf("parse release manifest %q: %w", path, err)
		}
		return manifest.Files, "json", nil
	}

	files, err := parseReleaseManifestText(trimmed)
	if err != nil {
		return nil, "", fmt.Errorf("parse release manifest %q as text: %w", path, err)
	}
	if err := validateReleaseManifestFiles(files); err != nil {
		return nil, "", fmt.Errorf("parse release manifest %q: %w", path, err)
	}
	return files, "text", nil
}

func parseReleaseManifestText(data []byte) ([]JSONReleaseManifestFile, error) {
	var files []JSONReleaseManifestFile
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		sha, rest, ok := cutReleaseManifestTextField(line)
		if !ok {
			return nil, fmt.Errorf("line %d: missing sha256", i+1)
		}
		sizeText, rest, ok := cutReleaseManifestTextField(rest)
		if !ok {
			return nil, fmt.Errorf("line %d: missing size", i+1)
		}
		artifactPath := strings.TrimLeft(rest, " \t")
		if artifactPath == "" {
			return nil, fmt.Errorf("line %d: missing path", i+1)
		}
		size, err := strconv.ParseInt(sizeText, 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("line %d: invalid size %q", i+1, sizeText)
		}
		files = append(files, JSONReleaseManifestFile{
			Path:      artifactPath,
			SizeBytes: size,
			SHA256:    strings.ToLower(sha),
		})
	}
	return files, nil
}

func cutReleaseManifestTextField(s string) (field, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return "", "", false
	}
	for i, r := range s {
		if r == ' ' || r == '\t' {
			return s[:i], s[i:], true
		}
	}
	return s, "", true
}

func validateReleaseManifestFiles(files []JSONReleaseManifestFile) error {
	seen := make(map[string]struct{}, len(files))
	for i, file := range files {
		if strings.TrimSpace(file.Path) == "" {
			return fmt.Errorf("file %d: empty path", i)
		}
		if file.SizeBytes < 0 {
			return fmt.Errorf("file %q: negative size", file.Path)
		}
		sha := strings.ToLower(strings.TrimSpace(file.SHA256))
		if len(sha) != sha256.Size*2 {
			return fmt.Errorf("file %q: invalid sha256 length", file.Path)
		}
		if _, err := hex.DecodeString(sha); err != nil {
			return fmt.Errorf("file %q: invalid sha256", file.Path)
		}
		cleanPath := strings.TrimSpace(file.Path)
		if _, ok := seen[cleanPath]; ok {
			return fmt.Errorf("file %q: duplicate path", file.Path)
		}
		seen[cleanPath] = struct{}{}
	}
	return nil
}

func verifyReleaseManifestFiles(manifestPath, format, baseDir string, files []JSONReleaseManifestFile) JSONReleaseManifestVerificationOutput {
	report := JSONReleaseManifestVerificationOutput{
		ManifestPath:   filepath.ToSlash(filepath.Clean(manifestPath)),
		ManifestFormat: format,
		BaseDir:        filepath.ToSlash(filepath.Clean(baseDir)),
		Checked:        len(files),
	}
	for _, expected := range files {
		result := verifyReleaseManifestFile(baseDir, expected)
		if result.OK {
			report.Matched++
		} else {
			switch result.Status {
			case "missing":
				report.Missing++
			case "hash_mismatch", "size_mismatch":
				report.Mismatched++
			default:
				report.Errors++
			}
		}
		report.Results = append(report.Results, result)
	}
	report.OK = report.Checked == report.Matched && report.Missing == 0 && report.Mismatched == 0 && report.Errors == 0
	return report
}

func verifyReleaseManifestFile(baseDir string, expected JSONReleaseManifestFile) JSONReleaseManifestVerificationFile {
	result := JSONReleaseManifestVerificationFile{
		Path:              expected.Path,
		Status:            "matched",
		ExpectedSizeBytes: expected.SizeBytes,
		ExpectedSHA256:    strings.ToLower(expected.SHA256),
	}

	artifactPath, err := releaseManifestArtifactPath(baseDir, expected.Path)
	if err != nil {
		result.Status = "invalid_path"
		result.Error = err.Error()
		return result
	}

	info, err := os.Stat(artifactPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Status = "missing"
		} else {
			result.Status = "error"
			result.Error = err.Error()
		}
		return result
	}
	if !info.Mode().IsRegular() {
		result.Status = "not_regular"
		result.Error = "not a regular file"
		return result
	}
	actualSize := info.Size()
	result.ActualSizeBytes = &actualSize

	actualSHA, err := hashReleaseFile(artifactPath)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	result.ActualSHA256 = actualSHA
	if actualSize != expected.SizeBytes {
		result.Status = "size_mismatch"
		return result
	}
	if actualSHA != strings.ToLower(expected.SHA256) {
		result.Status = "hash_mismatch"
		return result
	}
	result.OK = true
	return result
}

func releaseManifestArtifactPath(baseDir, manifestPath string) (string, error) {
	candidate := strings.TrimSpace(manifestPath)
	if candidate == "" {
		return "", fmt.Errorf("empty artifact path")
	}
	slashPath := strings.ReplaceAll(candidate, "\\", "/")
	if strings.HasPrefix(slashPath, "/") {
		return "", fmt.Errorf("absolute artifact path %q is not allowed", manifestPath)
	}
	cleanSlashPath := pathpkg.Clean(slashPath)
	if cleanSlashPath == "." || cleanSlashPath == ".." || strings.HasPrefix(cleanSlashPath, "../") {
		return "", fmt.Errorf("artifact path %q escapes base-dir", manifestPath)
	}
	localPath := filepath.FromSlash(cleanSlashPath)
	if filepath.IsAbs(localPath) || filepath.VolumeName(localPath) != "" {
		return "", fmt.Errorf("absolute artifact path %q is not allowed", manifestPath)
	}
	return filepath.Join(baseDir, localPath), nil
}

func printReleaseManifestVerificationReport(w io.Writer, report JSONReleaseManifestVerificationOutput) {
	fmt.Fprintf(w, "failed: verified %d/%d release artifact(s)\n", report.Matched, report.Checked)
	for _, result := range report.Results {
		if result.OK {
			continue
		}
		switch result.Status {
		case "missing":
			fmt.Fprintf(w, "missing: %s (expected sha256 %s, size %d)\n", result.Path, result.ExpectedSHA256, result.ExpectedSizeBytes)
		case "size_mismatch":
			actual := int64(0)
			if result.ActualSizeBytes != nil {
				actual = *result.ActualSizeBytes
			}
			fmt.Fprintf(w, "size_mismatch: %s (expected %d, got %d)\n", result.Path, result.ExpectedSizeBytes, actual)
		case "hash_mismatch":
			fmt.Fprintf(w, "hash_mismatch: %s (expected %s, got %s)\n", result.Path, result.ExpectedSHA256, result.ActualSHA256)
		default:
			if strings.TrimSpace(result.Error) != "" {
				fmt.Fprintf(w, "%s: %s: %s\n", result.Status, result.Path, result.Error)
			} else {
				fmt.Fprintf(w, "%s: %s\n", result.Status, result.Path)
			}
		}
	}
}

func releaseManifestVerificationError(report JSONReleaseManifestVerificationOutput) error {
	return fmt.Errorf(
		"release manifest verification failed: %d matched, %d missing, %d mismatched, %d error(s)",
		report.Matched,
		report.Missing,
		report.Mismatched,
		report.Errors,
	)
}

func runReleaseChecks(releaseVersion, changelogPath string) JSONReleaseCheckOutput {
	report := JSONReleaseCheckOutput{
		Version:       normalizeReleaseVersion(releaseVersion),
		ChangelogPath: filepath.ToSlash(filepath.Clean(changelogPath)),
	}
	if strings.TrimSpace(releaseVersion) == "" {
		report.Checks = append(report.Checks, JSONReleaseCheckResult{
			Name:    "version",
			OK:      false,
			Message: "release version is required",
		})
		report.OK = false
		return report
	}
	report.Checks = append(report.Checks, JSONReleaseCheckResult{
		Name:    "version",
		OK:      true,
		Message: fmt.Sprintf("checking %s", report.Version),
	})

	data, err := os.ReadFile(changelogPath)
	if err != nil {
		report.Checks = append(report.Checks, JSONReleaseCheckResult{
			Name:    "changelog",
			OK:      false,
			Message: fmt.Sprintf("read %s: %v", changelogPath, err),
		})
		report.OK = false
		return report
	}
	report.Checks = append(report.Checks, JSONReleaseCheckResult{
		Name:    "changelog",
		OK:      true,
		Message: fmt.Sprintf("found %s", changelogPath),
	})

	hasEntry, message := changelogHasVersionEntry(string(data), report.Version)
	report.Checks = append(report.Checks, JSONReleaseCheckResult{
		Name:    "changelog-entry",
		OK:      hasEntry,
		Message: message,
	})
	report.OK = true
	for _, check := range report.Checks {
		if !check.OK {
			report.OK = false
			break
		}
	}
	return report
}

func normalizeReleaseVersion(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return ""
	}
	return "v" + strings.TrimPrefix(trimmed, "v")
}

func changelogHasVersionEntry(changelog, version string) (bool, string) {
	want := normalizeReleaseVersion(version)
	lines := strings.Split(changelog, "\n")
	inSection := false
	hasContent := false
	for _, line := range lines {
		headingVersion, isReleaseHeading := changelogReleaseHeadingVersion(line)
		if isReleaseHeading {
			if inSection {
				break
			}
			if normalizeReleaseVersion(headingVersion) == want {
				inSection = true
			}
			continue
		}
		if inSection && strings.TrimSpace(line) != "" {
			hasContent = true
		}
	}
	if !inSection {
		return false, fmt.Sprintf("missing changelog section for %s", want)
	}
	if !hasContent {
		return false, fmt.Sprintf("changelog section for %s is empty", want)
	}
	return true, fmt.Sprintf("found changelog section for %s", want)
}

func changelogReleaseHeadingVersion(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
		return "", false
	}
	heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
	if heading == "" {
		return "", false
	}
	fields := strings.Fields(heading)
	if len(fields) == 0 {
		return "", false
	}
	version := strings.Trim(fields[0], "[]()")
	if version == "" {
		return "", false
	}
	return version, true
}

func releaseCheckError(report JSONReleaseCheckOutput) error {
	failures := 0
	for _, check := range report.Checks {
		if !check.OK {
			failures++
		}
	}
	return fmt.Errorf("release check failed for %s: %d failed check(s)", report.Version, failures)
}

type releaseSPDXDocument struct {
	SPDXVersion       string                    `json:"spdxVersion"`
	DataLicense       string                    `json:"dataLicense"`
	SPDXID            string                    `json:"SPDXID"`
	Name              string                    `json:"name"`
	DocumentNamespace string                    `json:"documentNamespace"`
	CreationInfo      releaseSPDXCreationInfo   `json:"creationInfo"`
	Files             []releaseSPDXFile         `json:"files"`
	Relationships     []releaseSPDXRelationship `json:"relationships"`
}

type releaseSPDXCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type releaseSPDXFile struct {
	FileName           string                `json:"fileName"`
	SPDXID             string                `json:"SPDXID"`
	Checksums          []releaseSPDXChecksum `json:"checksums"`
	FileTypes          []string              `json:"fileTypes,omitempty"`
	LicenseConcluded   string                `json:"licenseConcluded"`
	LicenseInfoInFiles []string              `json:"licenseInfoInFiles"`
	CopyrightText      string                `json:"copyrightText"`
}

type releaseSPDXChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type releaseSPDXRelationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

func buildReleaseSBOM(name string, files []JSONReleaseManifestFile) releaseSPDXDocument {
	docName := strings.TrimSpace(name)
	if docName == "" {
		docName = "graft-release"
	}
	created := time.Now().UTC().Format(time.RFC3339)
	doc := releaseSPDXDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              docName,
		DocumentNamespace: fmt.Sprintf("https://github.com/odvcencio/graft/sbom/%s/%d", spdxIDComponent(docName), time.Now().UTC().UnixNano()),
		CreationInfo: releaseSPDXCreationInfo{
			Created: created,
			Creators: []string{
				"Tool: graft-" + version,
			},
		},
	}
	for _, file := range files {
		fileID := releaseSPDXFileID(file)
		doc.Files = append(doc.Files, releaseSPDXFile{
			FileName: filepath.ToSlash(file.Path),
			SPDXID:   fileID,
			Checksums: []releaseSPDXChecksum{{
				Algorithm:     "SHA256",
				ChecksumValue: strings.ToLower(file.SHA256),
			}},
			FileTypes:          []string{"BINARY"},
			LicenseConcluded:   "NOASSERTION",
			LicenseInfoInFiles: []string{"NOASSERTION"},
			CopyrightText:      "NOASSERTION",
		})
		doc.Relationships = append(doc.Relationships, releaseSPDXRelationship{
			SPDXElementID:      "SPDXRef-DOCUMENT",
			RelationshipType:   "DESCRIBES",
			RelatedSPDXElement: fileID,
		})
	}
	return doc
}

func releaseSPDXFileID(file JSONReleaseManifestFile) string {
	prefix := strings.ToLower(file.SHA256)
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	component := spdxIDComponent(file.Path)
	if len(component) > 80 {
		component = component[:80]
	}
	if component == "" {
		component = "artifact"
	}
	return "SPDXRef-File-" + prefix + "-" + component
}

func spdxIDComponent(input string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(input) {
		valid := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' ||
			r == '-'
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

type releaseProvenanceStatement struct {
	Type          string                     `json:"_type"`
	Subject       []releaseProvenanceSubject `json:"subject"`
	PredicateType string                     `json:"predicateType"`
	Predicate     releaseSLSAPredicate       `json:"predicate"`
}

type releaseProvenanceSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type releaseSLSAPredicate struct {
	BuildDefinition releaseSLSABuildDefinition `json:"buildDefinition"`
	RunDetails      releaseSLSARunDetails      `json:"runDetails"`
}

type releaseSLSABuildDefinition struct {
	BuildType          string            `json:"buildType"`
	ExternalParameters map[string]string `json:"externalParameters,omitempty"`
	InternalParameters map[string]string `json:"internalParameters,omitempty"`
}

type releaseSLSARunDetails struct {
	Builder  releaseSLSABuilder  `json:"builder"`
	Metadata releaseSLSAMetadata `json:"metadata"`
}

type releaseSLSABuilder struct {
	ID string `json:"id"`
}

type releaseSLSAMetadata struct {
	InvocationID string `json:"invocationId"`
	StartedOn    string `json:"startedOn"`
	FinishedOn   string `json:"finishedOn"`
}

func buildReleaseProvenance(builderID, buildType string, files []JSONReleaseManifestFile) releaseProvenanceStatement {
	builderID = strings.TrimSpace(builderID)
	if builderID == "" {
		builderID = "https://github.com/odvcencio/graft"
	}
	buildType = strings.TrimSpace(buildType)
	if buildType == "" {
		buildType = "https://github.com/odvcencio/graft/release/v1"
	}
	now := time.Now().UTC()
	statement := releaseProvenanceStatement{
		Type:          "https://in-toto.io/Statement/v1",
		PredicateType: "https://slsa.dev/provenance/v1",
		Predicate: releaseSLSAPredicate{
			BuildDefinition: releaseSLSABuildDefinition{
				BuildType: buildType,
				ExternalParameters: map[string]string{
					"version":   version,
					"commit":    commit,
					"buildTime": buildTime,
					"goVersion": runtime.Version(),
				},
				InternalParameters: map[string]string{
					"tool": "graft",
				},
			},
			RunDetails: releaseSLSARunDetails{
				Builder: releaseSLSABuilder{ID: builderID},
				Metadata: releaseSLSAMetadata{
					InvocationID: fmt.Sprintf("graft-release-%d", now.UnixNano()),
					StartedOn:    now.Format(time.RFC3339),
					FinishedOn:   now.Format(time.RFC3339),
				},
			},
		},
	}
	for _, file := range files {
		statement.Subject = append(statement.Subject, releaseProvenanceSubject{
			Name: filepath.ToSlash(file.Path),
			Digest: map[string]string{
				"sha256": strings.ToLower(file.SHA256),
			},
		})
	}
	return statement
}

func signReleaseFiles(files []JSONReleaseManifestFile, signer repo.CommitSigner) (JSONReleaseSignOutput, error) {
	out := JSONReleaseSignOutput{
		SignedAt:        time.Now().UTC().Format(time.RFC3339),
		SignatureFormat: "sshsig-v1",
		PayloadFormat:   "graft-release-artifact-v1",
		Files:           make([]JSONReleaseSignatureFile, 0, len(files)),
	}
	for _, file := range files {
		payload := releaseSignaturePayload(JSONReleaseSignatureFile{
			Path:      file.Path,
			SizeBytes: file.SizeBytes,
			SHA256:    strings.ToLower(file.SHA256),
		})
		signature, err := signer(payload)
		if err != nil {
			return JSONReleaseSignOutput{}, fmt.Errorf("sign %s: %w", file.Path, err)
		}
		out.Files = append(out.Files, JSONReleaseSignatureFile{
			Path:      file.Path,
			SizeBytes: file.SizeBytes,
			SHA256:    strings.ToLower(file.SHA256),
			Signature: signature,
		})
	}
	return out, nil
}

func releaseSignaturePayload(file JSONReleaseSignatureFile) []byte {
	return []byte(fmt.Sprintf(
		"graft release artifact signature v1\npath %s\nsize %d\nsha256 %s\n",
		filepath.ToSlash(filepath.Clean(file.Path)),
		file.SizeBytes,
		strings.ToLower(file.SHA256),
	))
}

func loadReleaseSignatures(path string) (JSONReleaseSignOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return JSONReleaseSignOutput{}, fmt.Errorf("read release signature %q: %w", path, err)
	}
	var signatures JSONReleaseSignOutput
	if err := json.Unmarshal(data, &signatures); err != nil {
		return JSONReleaseSignOutput{}, fmt.Errorf("parse release signature %q: %w", path, err)
	}
	if signatures.SchemaVersion != 0 && signatures.SchemaVersion != JSONSchemaVersion {
		return JSONReleaseSignOutput{}, fmt.Errorf("parse release signature %q: unsupported schemaVersion %d", path, signatures.SchemaVersion)
	}
	if strings.TrimSpace(signatures.SignatureFormat) != "" && signatures.SignatureFormat != "sshsig-v1" {
		return JSONReleaseSignOutput{}, fmt.Errorf("parse release signature %q: unsupported signatureFormat %q", path, signatures.SignatureFormat)
	}
	if strings.TrimSpace(signatures.PayloadFormat) != "" && signatures.PayloadFormat != "graft-release-artifact-v1" {
		return JSONReleaseSignOutput{}, fmt.Errorf("parse release signature %q: unsupported payloadFormat %q", path, signatures.PayloadFormat)
	}
	if err := validateReleaseSignatureFiles(signatures.Files); err != nil {
		return JSONReleaseSignOutput{}, fmt.Errorf("parse release signature %q: %w", path, err)
	}
	return signatures, nil
}

func validateReleaseSignatureFiles(files []JSONReleaseSignatureFile) error {
	manifestFiles := make([]JSONReleaseManifestFile, 0, len(files))
	for _, file := range files {
		if strings.TrimSpace(file.Signature) == "" {
			return fmt.Errorf("file %q: missing signature", file.Path)
		}
		manifestFiles = append(manifestFiles, JSONReleaseManifestFile{
			Path:      file.Path,
			SizeBytes: file.SizeBytes,
			SHA256:    file.SHA256,
		})
	}
	return validateReleaseManifestFiles(manifestFiles)
}

func loadReleaseAllowedSigners(path string) (map[string][]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("release allowed signers %q: %w", path, err)
	}
	signers, err := repo.LoadAllowedSigners(path)
	if err != nil {
		return nil, err
	}
	return signers, nil
}

func verifyReleaseSignatures(signaturePath, baseDir string, files []JSONReleaseSignatureFile, allowedSigners map[string][]byte) JSONReleaseVerifySignatureOutput {
	report := JSONReleaseVerifySignatureOutput{
		SignaturePath: filepath.ToSlash(filepath.Clean(signaturePath)),
		BaseDir:       filepath.ToSlash(filepath.Clean(baseDir)),
		Checked:       len(files),
	}
	for _, file := range files {
		result := verifyReleaseSignatureFile(baseDir, file, allowedSigners)
		if result.OK {
			report.Valid++
		} else {
			switch result.Status {
			case "missing":
				report.Missing++
			case "size_mismatch", "hash_mismatch":
				report.Mismatched++
			case "signature_invalid", "signature_untrusted":
				report.Invalid++
			default:
				report.Errors++
			}
		}
		report.Results = append(report.Results, result)
	}
	report.OK = report.Checked == report.Valid && report.Missing == 0 && report.Mismatched == 0 && report.Invalid == 0 && report.Errors == 0
	return report
}

func verifyReleaseSignatureFile(baseDir string, file JSONReleaseSignatureFile, allowedSigners map[string][]byte) JSONReleaseSignatureVerificationResult {
	result := JSONReleaseSignatureVerificationResult{
		Path:   file.Path,
		Status: "valid",
	}
	artifactPath, err := releaseManifestArtifactPath(baseDir, file.Path)
	if err != nil {
		result.Status = "invalid_path"
		result.Error = err.Error()
		return result
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Status = "missing"
		} else {
			result.Status = "error"
			result.Error = err.Error()
		}
		return result
	}
	if !info.Mode().IsRegular() {
		result.Status = "not_regular"
		result.Error = "not a regular file"
		return result
	}
	actualSHA, err := hashReleaseFile(artifactPath)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	if info.Size() != file.SizeBytes {
		result.Status = "size_mismatch"
		return result
	}
	if actualSHA != strings.ToLower(file.SHA256) {
		result.Status = "hash_mismatch"
		return result
	}
	pubKeyData, algorithm, fingerprint, err := releaseSignaturePublicKey(file.Signature)
	if err != nil {
		result.Status = "signature_invalid"
		result.Error = err.Error()
		return result
	}
	result.Algorithm = algorithm
	result.SignerKey = fingerprint
	if err := repo.VerifySSHSignature(releaseSignaturePayload(file), file.Signature, pubKeyData); err != nil {
		result.Status = "signature_invalid"
		result.Error = err.Error()
		return result
	}
	if allowedSigners != nil {
		trusted, signerName := releaseSignatureAllowed(pubKeyData, allowedSigners)
		if !trusted {
			result.Status = "signature_untrusted"
			result.Error = "signature valid but key not in allowed signers"
			return result
		}
		if signerName != "" {
			result.SignerKey = signerName
		}
	}
	result.OK = true
	return result
}

func releaseSignatureAllowed(pubKeyData []byte, allowedSigners map[string][]byte) (bool, string) {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(pubKeyData)
	if err != nil {
		return false, ""
	}
	pubKeyBytes := pubKey.Marshal()
	for name, authKeyLine := range allowedSigners {
		allowedPub, _, _, _, err := ssh.ParseAuthorizedKey(authKeyLine)
		if err != nil {
			continue
		}
		if bytes.Equal(allowedPub.Marshal(), pubKeyBytes) {
			return true, name
		}
	}
	return false, ""
}

func releaseSignaturePublicKey(signature string) ([]byte, string, string, error) {
	parts := strings.SplitN(signature, ":", 4)
	if len(parts) != 4 {
		return nil, "", "", fmt.Errorf("invalid signature format: expected 4 colon-separated parts")
	}
	if parts[0] != "sshsig-v1" {
		return nil, "", "", fmt.Errorf("invalid signature prefix: %q", parts[0])
	}
	pubKeyBytes, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, "", "", fmt.Errorf("decode public key: %w", err)
	}
	pubKey, err := ssh.ParsePublicKey(pubKeyBytes)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse public key: %w", err)
	}
	return ssh.MarshalAuthorizedKey(pubKey), parts[1], ssh.FingerprintSHA256(pubKey), nil
}

func printReleaseSignatureVerificationReport(w io.Writer, report JSONReleaseVerifySignatureOutput) {
	fmt.Fprintf(w, "failed: verified %d/%d release signature(s)\n", report.Valid, report.Checked)
	for _, result := range report.Results {
		if result.OK {
			continue
		}
		if strings.TrimSpace(result.Error) != "" {
			fmt.Fprintf(w, "%s: %s: %s\n", result.Status, result.Path, result.Error)
		} else {
			fmt.Fprintf(w, "%s: %s\n", result.Status, result.Path)
		}
	}
}

func releaseSignatureVerificationError(report JSONReleaseVerifySignatureOutput) error {
	return fmt.Errorf(
		"release signature verification failed: %d valid, %d missing, %d mismatched, %d invalid, %d error(s)",
		report.Valid,
		report.Missing,
		report.Mismatched,
		report.Invalid,
		report.Errors,
	)
}
