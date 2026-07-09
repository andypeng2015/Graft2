package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/redact"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var jsonFlag bool
	var bundleFlag bool
	var globalFlag bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose repository health without modifying data",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if globalFlag {
				if bundleFlag {
					return usageError(cmd, fmt.Errorf("--global cannot be used with --bundle"))
				}
				return runGlobalDoctor(cmd, jsonFlag)
			}

			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				if !jsonFlag && !bundleFlag && isNotGraftRepositoryError(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "no graft repository found; running global install preflight")
					return runGlobalDoctor(cmd, false)
				}
				return err
			}

			report := r.VerifyIntegrity()
			addDoctorRepositoryDiagnostics(r, report)
			if bundleFlag {
				if err := writeJSON(cmd.OutOrStdout(), doctorBundleToJSON(r, report)); err != nil {
					return err
				}
				if !report.OK {
					return repositoryNeedsRepairError(fmt.Errorf("doctor: repository has integrity errors"))
				}
				return nil
			}
			if jsonFlag {
				if err := writeJSON(cmd.OutOrStdout(), doctorReportToJSON(report)); err != nil {
					return err
				}
				if !report.OK {
					return repositoryNeedsRepairError(fmt.Errorf("doctor: repository has integrity errors"))
				}
				return nil
			}

			printDoctorReport(cmd.OutOrStdout(), report)
			if !report.OK {
				return repositoryNeedsRepairError(fmt.Errorf("doctor: repository has integrity errors"))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().BoolVar(&bundleFlag, "bundle", false, "output a redacted JSON support bundle")
	cmd.Flags().BoolVar(&globalFlag, "global", false, "check the installed binary, git, and user config without opening a repository")
	return cmd
}

func runGlobalDoctor(cmd *cobra.Command, jsonFlag bool) error {
	report := doctorGlobalReport(cmd.Context())
	if jsonFlag {
		if err := writeJSON(cmd.OutOrStdout(), report); err != nil {
			return err
		}
		if !report.OK {
			return doctorGlobalFailure()
		}
		return nil
	}

	printDoctorGlobalReport(cmd.OutOrStdout(), report)
	if !report.OK {
		return doctorGlobalFailure()
	}
	return nil
}

func doctorGlobalFailure() error {
	return newCommandError(
		errorCodeVerificationFailed,
		exitVerificationFailure,
		fmt.Errorf("doctor global: install preflight has errors"),
		"inspect diagnostics; install git or fix user config permissions",
	)
}

func doctorGlobalReport(ctx context.Context) JSONDoctorGlobalOutput {
	out := JSONDoctorGlobalOutput{
		GeneratedAt:                    time.Now().UTC().Format(time.RFC3339),
		Version:                        version,
		Commit:                         commit,
		BuildTime:                      buildTime,
		GoVersion:                      runtime.Version(),
		OS:                             runtime.GOOS,
		Arch:                           runtime.GOARCH,
		SupportedRepositoryFormat:      repo.RepositoryFormatVersion,
		SupportedRemoteProtocolVersion: remote.ProtocolVersion,
		Git:                            doctorGlobalGit(ctx),
	}

	bundle := JSONDoctorBundleOutput{}
	out.UserConfig = doctorBundleUserConfig(&bundle)
	out.CollectionErrors = bundle.CollectionErrors

	if !out.Git.Found {
		out.Diagnostics = append(out.Diagnostics, JSONRepositoryDiagnostic{
			Severity: "error",
			Code:     "git_not_found",
			Message:  "git executable was not found in PATH; default graft init uses a Git shadow repository",
			Repair:   "install git, fix PATH, or use graft init --no-git for a pure graft repository",
		})
	} else if out.Git.Error != "" {
		out.Diagnostics = append(out.Diagnostics, JSONRepositoryDiagnostic{
			Severity: "error",
			Code:     "git_version_failed",
			Message:  out.Git.Error,
			Repair:   "verify git is executable with `git --version`",
		})
	}

	if out.UserConfig.ConfigFilePresent && !out.UserConfig.ConfigFileSecure {
		out.Diagnostics = append(out.Diagnostics, JSONRepositoryDiagnostic{
			Severity: "error",
			Code:     "user_config_permissions_insecure",
			Message:  out.UserConfig.ConfigFileWarning,
			Repair:   out.UserConfig.ConfigFileRepair,
		})
	}
	if out.UserConfig.LoadError != "" {
		out.Diagnostics = append(out.Diagnostics, JSONRepositoryDiagnostic{
			Severity: "error",
			Code:     "user_config_load_failed",
			Message:  out.UserConfig.LoadError,
			Repair:   "inspect ~/.graftconfig or rerun graft auth setup",
		})
	}
	for _, collectionErr := range out.CollectionErrors {
		out.Diagnostics = append(out.Diagnostics, JSONRepositoryDiagnostic{
			Severity: "error",
			Code:     "global_doctor_collection_error",
			Message:  collectionErr.Section + ": " + redact.Text(collectionErr.Error),
		})
	}
	if !out.UserConfig.NameConfigured {
		out.Diagnostics = append(out.Diagnostics, JSONRepositoryDiagnostic{
			Severity: "warning",
			Code:     "user_name_not_configured",
			Message:  "global user.name is not configured; commits may need --author or repo-local config",
			Repair:   `graft config --global user.name "Your Name"`,
		})
	}
	if !out.UserConfig.EmailConfigured {
		out.Diagnostics = append(out.Diagnostics, JSONRepositoryDiagnostic{
			Severity: "warning",
			Code:     "user_email_not_configured",
			Message:  "global user.email is not configured; commits may need --author or repo-local config",
			Repair:   `graft config --global user.email "you@example.com"`,
		})
	}

	out.OK = !diagnosticsContainSeverity(out.Diagnostics, "error")
	return out
}

func doctorGlobalGit(ctx context.Context) JSONDoctorGlobalTool {
	out := JSONDoctorGlobalTool{Name: "git"}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Found = true
	out.Path = gitPath
	raw, err := runGitCaptureWithLabel(ctx, os.TempDir(), "doctor-git-version", "--version")
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Version = strings.TrimSpace(string(raw))
	return out
}

func diagnosticsContainSeverity(diagnostics []JSONRepositoryDiagnostic, severity string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == severity {
			return true
		}
	}
	return false
}

func isNotGraftRepositoryError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not a graft repository")
}

func addDoctorRepositoryDiagnostics(r *repo.Repo, report *repo.RepositoryIntegrityReport) {
	if report == nil {
		return
	}
	for _, diagnostic := range doctorRemoteTransportDiagnostics(r) {
		report.Diagnostics = append(report.Diagnostics, diagnostic)
		if diagnostic.Severity == repo.DiagnosticError {
			report.OK = false
		}
	}
	sort.SliceStable(report.Diagnostics, func(i, j int) bool {
		si := doctorDiagnosticSeverityRank(report.Diagnostics[i].Severity)
		sj := doctorDiagnosticSeverityRank(report.Diagnostics[j].Severity)
		if si != sj {
			return si > sj
		}
		if report.Diagnostics[i].Code != report.Diagnostics[j].Code {
			return report.Diagnostics[i].Code < report.Diagnostics[j].Code
		}
		return report.Diagnostics[i].Message < report.Diagnostics[j].Message
	})
}

func doctorRemoteTransportDiagnostics(r *repo.Repo) []repo.RepositoryDiagnostic {
	cfg, err := r.ReadConfig()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Remotes))
	for name := range cfg.Remotes {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []repo.RepositoryDiagnostic
	for _, name := range names {
		rawURL := strings.TrimSpace(cfg.Remotes[name])
		if rawURL == "" {
			continue
		}
		remoteURL := rawURL
		if _, canonical, err := parseRemoteSpec(rawURL); err == nil {
			remoteURL = canonical
		}
		warning := remoteTransportTrustWarning(remoteURL)
		if warning == "" {
			continue
		}
		out = append(out, repo.RepositoryDiagnostic{
			Severity:  repo.DiagnosticWarning,
			Code:      "remote_transport_insecure",
			Message:   fmt.Sprintf("remote %q: %s", name, warning),
			Repair:    fmt.Sprintf("graft remote set-url %s https://... (or use --allow-insecure only for trusted development remotes)", name),
			Operation: "remote",
		})
	}
	return out
}

func doctorDiagnosticSeverityRank(severity repo.DiagnosticSeverity) int {
	switch severity {
	case repo.DiagnosticError:
		return 3
	case repo.DiagnosticWarning:
		return 2
	default:
		return 1
	}
}

func printDoctorGlobalReport(out io.Writer, report JSONDoctorGlobalOutput) {
	if report.OK {
		fmt.Fprintln(out, "ok: graft install preflight passed")
	} else {
		fmt.Fprintln(out, "graft install preflight: errors found")
	}
	fmt.Fprintf(out, "graft: %s (commit %s, built %s)\n", report.Version, report.Commit, report.BuildTime)
	fmt.Fprintf(out, "go: %s %s/%s\n", report.GoVersion, report.OS, report.Arch)
	if report.Git.Found {
		versionText := report.Git.Version
		if versionText == "" {
			versionText = "version unavailable"
		}
		fmt.Fprintf(out, "git: %s (%s)\n", versionText, report.Git.Path)
	} else {
		fmt.Fprintf(out, "git: not found\n")
	}
	if report.UserConfig.ConfigFilePresent {
		state := "secure"
		if !report.UserConfig.ConfigFileSecure {
			state = "insecure"
		}
		fmt.Fprintf(out, "user config: present, %s", state)
		if report.UserConfig.ConfigFileMode != "" {
			fmt.Fprintf(out, " (%s)", report.UserConfig.ConfigFileMode)
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "user config: not present")
	}
	if report.UserConfig.Loaded {
		fmt.Fprintf(out, "identity: name=%t email=%t authToken=%t profiles=%d\n",
			report.UserConfig.NameConfigured,
			report.UserConfig.EmailConfigured,
			report.UserConfig.TokenSet,
			len(report.UserConfig.Profiles),
		)
	}
	printDoctorGlobalDiagnostics(out, report.Diagnostics)
}

func printDoctorGlobalDiagnostics(out io.Writer, diagnostics []JSONRepositoryDiagnostic) {
	if len(diagnostics) == 0 {
		return
	}
	for _, d := range diagnostics {
		fmt.Fprintf(out, "%s [%s]: %s\n", d.Severity, d.Code, d.Message)
		if d.Repair != "" {
			fmt.Fprintf(out, "  repair: %s\n", d.Repair)
		}
	}
}

func doctorReportToJSON(report *repo.RepositoryIntegrityReport) JSONDoctorOutput {
	out := JSONDoctorOutput{
		SchemaVersion: JSONSchemaVersion,
		OK:            report.OK,
		LooseObjects:  report.LooseObjects,
		PackFiles:     report.PackFiles,
		PackObjects:   report.PackObjects,
	}
	for _, d := range report.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, repositoryDiagnosticToJSON(d))
	}
	return out
}

func printDoctorReport(out io.Writer, report *repo.RepositoryIntegrityReport) {
	if report.OK {
		fmt.Fprintf(out, "ok: repository metadata and objects verified\n")
	} else {
		fmt.Fprintf(out, "repository health: errors found\n")
	}
	fmt.Fprintf(
		out,
		"objects: %d loose, %d pack file(s), %d packed object(s)\n",
		report.LooseObjects,
		report.PackFiles,
		report.PackObjects,
	)
	if len(report.Diagnostics) == 0 {
		return
	}
	for _, d := range report.Diagnostics {
		fmt.Fprintf(out, "%s [%s]: %s\n", d.Severity, d.Code, d.Message)
		if d.Ref != "" {
			fmt.Fprintf(out, "  ref: %s\n", d.Ref)
		}
		if d.Path != "" {
			fmt.Fprintf(out, "  path: %s\n", d.Path)
		}
		if d.Object != "" {
			fmt.Fprintf(out, "  object: %s\n", d.Object)
		}
		if d.Repair != "" {
			fmt.Fprintf(out, "  repair: %s\n", d.Repair)
		}
	}
}

func doctorBundleToJSON(r *repo.Repo, report *repo.RepositoryIntegrityReport) JSONDoctorBundleOutput {
	bundle := JSONDoctorBundleOutput{
		SchemaVersion: JSONSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Verify:        doctorReportToJSON(report),
		Environment: JSONDoctorBundleEnvironment{
			GoVersion: runtime.Version(),
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
		},
		Protocol: doctorBundleProtocol(r),
		Redaction: JSONDoctorBundleRedaction{
			SecretsIncluded: false,
			SourceIncluded:  false,
			Notes: []string{
				"tokens, passwords, userinfo, signing-key paths, and source contents are omitted or redacted",
			},
		},
	}

	bundle.Repository = doctorBundleRepository(r, &bundle)
	bundle.UserConfig = doctorBundleUserConfig(&bundle)
	bundle.Hooks = doctorBundleHooks(r, &bundle)

	if status, err := r.GitShadowStatus(); err != nil {
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "gitShadow",
			Error:   redact.Text(err.Error()),
		})
		bundle.GitShadow = repairGitShadowStatusToJSON(status)
	} else {
		bundle.GitShadow = repairGitShadowStatusToJSON(status)
	}

	if entries, err := r.ReadReflog("HEAD", 10); err != nil {
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "recentReflog",
			Error:   redact.Text(err.Error()),
		})
	} else {
		for _, entry := range entries {
			bundle.RecentReflog = append(bundle.RecentReflog, JSONDoctorBundleReflogEntry{
				Ref:       entry.Ref,
				OldHash:   string(entry.OldHash),
				NewHash:   string(entry.NewHash),
				Timestamp: entry.Timestamp,
				Reason:    redact.Text(entry.Reason),
			})
		}
	}

	return bundle
}

func doctorBundleProtocol(r *repo.Repo) JSONDoctorBundleProtocol {
	contract := remote.SupportedProtocolContract()
	out := JSONDoctorBundleProtocol{
		SupportedRepositoryFormat:      repo.RepositoryFormatVersion,
		SupportedRemoteProtocolVersion: contract.ProtocolVersion,
		Documentation:                  contract.Documentation,
		ClientCapabilities:             append([]string(nil), contract.ClientCapabilities...),
		TransportCount:                 len(contract.Transports),
		EndpointCount:                  len(contract.Endpoints),
	}
	for _, capability := range contract.DefinedCapabilities {
		out.DefinedCapabilities = append(out.DefinedCapabilities, capability.Name)
	}
	for _, limit := range contract.ServerLimits {
		out.ServerLimitKeys = append(out.ServerLimitKeys, limit.Name)
	}
	for _, limit := range contract.ResponseLimits {
		out.ResponseLimits = append(out.ResponseLimits, JSONDoctorBundleProtocolResponseLimit{
			Name:  limit.Name,
			Bytes: limit.Bytes,
		})
	}
	if r != nil {
		for _, diagnostic := range doctorRemoteTransportDiagnostics(r) {
			out.Diagnostics = append(out.Diagnostics, repositoryDiagnosticToJSON(diagnostic))
		}
	}
	return out
}

func doctorBundleRepository(r *repo.Repo, bundle *JSONDoctorBundleOutput) JSONDoctorBundleRepository {
	out := JSONDoctorBundleRepository{}
	if cfg, err := r.ReadRepositoryConfig(); err != nil {
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "repositoryConfig",
			Error:   redact.Text(err.Error()),
		})
	} else {
		out.RepositoryFormatVersion = cfg.RepositoryFormatVersion
		out.ObjectHash = cfg.ObjectHash
		out.Features = cfg.Features
	}

	if branch, err := r.CurrentBranch(); err == nil {
		out.CurrentBranch = branch
	}
	if head, err := r.ResolveRef("HEAD"); err == nil {
		out.Head = string(head)
	}

	cfg, err := r.ReadConfig()
	if err != nil {
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "repoConfig",
			Error:   redact.Text(err.Error()),
		})
		return out
	}
	if cfg.User != nil {
		out.User.NameConfigured = strings.TrimSpace(cfg.User.Name) != ""
		out.User.EmailConfigured = strings.TrimSpace(cfg.User.Email) != ""
	}
	remoteNames := make([]string, 0, len(cfg.Remotes))
	for name := range cfg.Remotes {
		remoteNames = append(remoteNames, name)
	}
	sort.Strings(remoteNames)
	for _, name := range remoteNames {
		out.Remotes = append(out.Remotes, JSONDoctorBundleRemote{
			Name: name,
			URL:  redactSupportURL(cfg.Remotes[name]),
		})
	}
	return out
}

func doctorBundleHooks(r *repo.Repo, bundle *JSONDoctorBundleOutput) JSONDoctorBundleHooks {
	out := JSONDoctorBundleHooks{}
	cfg, err := r.ReadConfig()
	if err != nil {
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "hookTrust",
			Error:   redact.Text(err.Error()),
		})
	} else if cfg.Hooks != nil {
		out.Configured = true
		out.Trusted = cfg.Hooks.Trusted
		out.TrustedAt = cfg.Hooks.TrustedAt
	}

	hooksTomlPath := filepath.Join(r.RootDir, "hooks.toml")
	if _, err := os.Stat(hooksTomlPath); err == nil {
		out.HooksTomlPresent = true
	} else if err != nil && !os.IsNotExist(err) {
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "hooksToml",
			Error:   redact.Text(err.Error()),
		})
	}

	hooksDir := filepath.Join(r.GraftDir, "hooks")
	entries, err := os.ReadDir(hooksDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
					Section: "hooksDir",
					Error:   redact.Text(infoErr.Error()),
				})
				continue
			}
			if info.Mode()&0o111 != 0 {
				out.ExecutableHooks = append(out.ExecutableHooks, entry.Name())
			}
		}
		sort.Strings(out.ExecutableHooks)
	} else if !os.IsNotExist(err) {
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "hooksDir",
			Error:   redact.Text(err.Error()),
		})
	}

	if !out.Trusted && (out.HooksTomlPresent || len(out.ExecutableHooks) > 0) {
		out.Warning = "repo-provided hooks are present but untrusted and will be skipped"
		out.Repair = "graft config hooks.trusted true"
	}
	return out
}

func doctorBundleUserConfig(bundle *JSONDoctorBundleOutput) JSONDoctorBundleUserConfig {
	out := JSONDoctorBundleUserConfig{}
	status, statusErr := userconfig.CheckPermissions()
	if statusErr == nil {
		out.ConfigFilePresent = status.Exists
		out.ConfigFileSecure = status.Secure
		out.ConfigFileWarning = status.Warning
		out.ConfigFileRepair = status.Repair
		if status.Exists {
			out.ConfigFileMode = status.Mode.String()
		}
	} else {
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "userConfigPermissions",
			Error:   redact.Text(statusErr.Error()),
		})
	}

	cfg, err := userconfig.Load()
	if err != nil {
		out.LoadError = redact.Text(err.Error())
		bundle.CollectionErrors = append(bundle.CollectionErrors, JSONDoctorBundleCollectionError{
			Section: "userConfig",
			Error:   redact.Text(err.Error()),
		})
		return out
	}
	out.Loaded = true
	out.NameConfigured = strings.TrimSpace(cfg.Name) != ""
	out.EmailConfigured = strings.TrimSpace(cfg.Email) != ""
	out.DefaultOrchardURL = redactSupportURL(cfg.DefaultOrchardURL())
	out.TokenSet = strings.TrimSpace(cfg.Token) != ""
	out.UsernameConfigured = strings.TrimSpace(cfg.Username) != ""
	out.OwnerConfigured = strings.TrimSpace(cfg.Owner) != ""
	out.SigningKeyConfigured = strings.TrimSpace(cfg.SigningKeyPath) != ""
	out.AutoSign = cfg.AutoSign
	out.Workspaces = len(cfg.Workspaces)

	for _, host := range cfg.OrchardProfileHosts() {
		profile := cfg.OrchardProfile(host)
		out.Profiles = append(out.Profiles, JSONDoctorBundleOrchardProfile{
			Host:               redactSupportURL(host),
			TokenSet:           strings.TrimSpace(profile.Token) != "",
			UsernameConfigured: strings.TrimSpace(profile.Username) != "",
			OwnerConfigured:    strings.TrimSpace(profile.Owner) != "",
		})
	}

	return out
}

func redactSupportURL(raw string) string {
	return redact.URL(raw)
}
