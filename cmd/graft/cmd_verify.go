package main

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newVerifyCmd() *cobra.Command {
	var signatures bool
	var jsonFlag bool
	var requireSigned bool
	var allowedSignersPath string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify object integrity and commit signatures",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}

			if signatures {
				return verifyBranchSignatures(cmd, r, jsonFlag, requireSigned, allowedSignersPath)
			}
			if requireSigned || strings.TrimSpace(allowedSignersPath) != "" {
				return usageError(cmd, fmt.Errorf("--require-signed and --allowed-signers require --signatures"))
			}

			// Default: verify object store and repository metadata integrity.
			report := r.VerifyIntegrity()

			if jsonFlag {
				if err := writeJSON(cmd.OutOrStdout(), verifyReportToJSON(report)); err != nil {
					return err
				}
				if !report.OK {
					return verificationFailureError(fmt.Errorf("verify: repository has integrity errors"))
				}
				return nil
			}

			if !report.OK {
				for _, d := range report.Diagnostics {
					if d.Severity == repo.DiagnosticError {
						fmt.Fprintf(cmd.OutOrStdout(), "error [%s]: %s\n", d.Code, d.Message)
					}
				}
				return verificationFailureError(firstVerifyError(report))
			}

			fmt.Fprintf(
				cmd.OutOrStdout(),
				"ok: verified %d loose object(s), %d pack file(s), %d packed object(s)\n",
				report.LooseObjects,
				report.PackFiles,
				report.PackObjects,
			)
			for _, d := range report.Diagnostics {
				if d.Severity == repo.DiagnosticWarning {
					fmt.Fprintf(cmd.OutOrStdout(), "warning [%s]: %s\n", d.Code, d.Message)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&signatures, "signatures", false, "Verify commit signatures on current branch (up to 100)")
	cmd.Flags().BoolVar(&requireSigned, "require-signed", false, "fail if any checked commit is unsigned or has an invalid signature")
	cmd.Flags().StringVar(&allowedSignersPath, "allowed-signers", "", "OpenSSH allowed_signers file for trusted commit keys")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	// Add the "commit" subcommand.
	cmd.AddCommand(newVerifyCommitCmd())
	cmd.AddCommand(newVerifyPushLimitsCmd())

	return cmd
}

func newVerifyCommitCmd() *cobra.Command {
	var jsonFlag bool
	var requireSigned bool
	var allowedSignersPath string

	cmd := &cobra.Command{
		Use:   "commit <hash>",
		Short: "Verify a single commit's signature",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}

			hash := object.Hash(args[0])
			result, err := verifyCommitSignatureForCLI(r, hash, allowedSignersPath)
			if err != nil {
				return err
			}
			results := []repo.VerificationResult{*result}
			ok := signatureResultsOK(results, requireSigned)

			if jsonFlag {
				if err := writeJSON(cmd.OutOrStdout(), signatureResultsToJSON(results, ok, requireSigned, allowedSignersPath)); err != nil {
					return err
				}
				if !ok {
					return verificationFailureError(signatureVerificationError(results, requireSigned))
				}
				return nil
			}

			printVerificationResult(cmd, result)
			if !ok {
				return verificationFailureError(signatureVerificationError(results, requireSigned))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().BoolVar(&requireSigned, "require-signed", false, "fail if the commit is unsigned or has an invalid signature")
	cmd.Flags().StringVar(&allowedSignersPath, "allowed-signers", "", "OpenSSH allowed_signers file for trusted commit keys")

	return cmd
}

func newVerifyPushLimitsCmd() *cobra.Command {
	var jsonFlag bool
	var remoteName string

	cmd := &cobra.Command{
		Use:   "push-limits [ref]",
		Short: "Check whether a ref can be pushed under the graft object-size limits",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}

			refName := ""
			if len(args) > 0 {
				refName = args[0]
			}
			pushTarget, localRef, remoteRef, err := resolvePushRefNames(r, refName)
			if err != nil {
				return err
			}

			remoteURL := ""
			if strings.TrimSpace(remoteName) != "" {
				name, url, transport, err := resolveRemoteNameAndSpec(r, remoteName)
				if err != nil {
					return err
				}
				if transport == remoteTransportGit {
					return fmt.Errorf("verify push-limits currently supports orchard/graft remotes only")
				}
				remoteName = name
				remoteURL = url
			}

			report, err := collectPushLimitReport(cmd.Context(), r, pushTarget, localRef, remoteName, remoteURL, remoteRef)
			if err != nil {
				return err
			}

			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), jsonVerifyPushLimitReport(report))
			}

			if err := pushLimitError(report); err != nil {
				return verificationFailureError(err)
			}
			printPushLimitSummary(cmd.OutOrStdout(), report)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().StringVar(&remoteName, "remote", "", "remote name or orchard/graft URL used to compute the push set")
	return cmd
}

func verifyBranchSignatures(cmd *cobra.Command, r *repo.Repo, jsonFlag bool, requireSigned bool, allowedSignersPath string) error {
	results, err := collectBranchSignatureResults(r, 100, allowedSignersPath)
	if err != nil {
		return err
	}
	ok := signatureResultsOK(results, requireSigned)

	if jsonFlag {
		if err := writeJSON(cmd.OutOrStdout(), signatureResultsToJSON(results, ok, requireSigned, allowedSignersPath)); err != nil {
			return err
		}
		if !ok {
			return verificationFailureError(signatureVerificationError(results, requireSigned))
		}
		return nil
	}

	for i := range results {
		printVerificationResult(cmd, &results[i])
	}
	if !ok {
		return verificationFailureError(signatureVerificationError(results, requireSigned))
	}
	return nil
}

func collectBranchSignatureResults(r *repo.Repo, limit int, allowedSignersPath string) ([]repo.VerificationResult, error) {
	head, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("verify branch: resolve HEAD: %w", err)
	}
	return collectSignatureResultsFromHash(r, head, limit, allowedSignersPath)
}

func collectSignatureResultsFromRef(r *repo.Repo, ref string, limit int, allowedSignersPath string) ([]repo.VerificationResult, error) {
	head, err := r.ResolveRef(ref)
	if err != nil {
		return nil, fmt.Errorf("verify branch: resolve %s: %w", ref, err)
	}
	return collectSignatureResultsFromHash(r, head, limit, allowedSignersPath)
}

func collectSignatureResultsFromHash(r *repo.Repo, head object.Hash, limit int, allowedSignersPath string) ([]repo.VerificationResult, error) {
	if strings.TrimSpace(allowedSignersPath) == "" {
		return collectEmbeddedSignatureResultsFromHash(r, head, limit)
	}

	signers, err := repo.LoadAllowedSigners(allowedSignersPath)
	if err != nil {
		return nil, err
	}

	commits, err := r.Log(head, limit)
	if err != nil {
		return nil, fmt.Errorf("verify branch: log: %w", err)
	}

	results := make([]repo.VerificationResult, 0, len(commits))
	current := head
	for i := 0; i < len(commits); i++ {
		commit, err := r.Store.ReadCommit(current)
		if err != nil {
			return nil, fmt.Errorf("verify branch: read commit %s: %w", current, err)
		}
		result, err := repo.VerifyCommitAgainstAllowedSigners(commit, signers)
		if err != nil {
			return nil, err
		}
		result.CommitHash = current
		results = append(results, *result)

		if len(commits[i].Parents) == 0 {
			break
		}
		current = commits[i].Parents[0]
	}
	return results, nil
}

func collectEmbeddedSignatureResultsFromHash(r *repo.Repo, head object.Hash, limit int) ([]repo.VerificationResult, error) {
	commits, err := r.Log(head, limit)
	if err != nil {
		return nil, fmt.Errorf("verify branch: log: %w", err)
	}

	results := make([]repo.VerificationResult, 0, len(commits))
	current := head
	for i := 0; i < len(commits); i++ {
		vr, err := r.VerifyCommitSignature(current)
		if err != nil {
			return nil, err
		}
		results = append(results, *vr)

		if len(commits[i].Parents) == 0 {
			break
		}
		current = commits[i].Parents[0]
	}
	return results, nil
}

func verifyCommitSignatureForCLI(r *repo.Repo, hash object.Hash, allowedSignersPath string) (*repo.VerificationResult, error) {
	if strings.TrimSpace(allowedSignersPath) == "" {
		return r.VerifyCommitSignature(hash)
	}
	signers, err := repo.LoadAllowedSigners(allowedSignersPath)
	if err != nil {
		return nil, err
	}
	commit, err := r.Store.ReadCommit(hash)
	if err != nil {
		return nil, fmt.Errorf("verify: read commit %s: %w", hash, err)
	}
	result, err := repo.VerifyCommitAgainstAllowedSigners(commit, signers)
	if err != nil {
		return nil, err
	}
	result.CommitHash = hash
	return result, nil
}

func signatureResultsToJSON(results []repo.VerificationResult, ok bool, requireSigned bool, allowedSignersPath string) JSONVerifyOutput {
	jsonResults := make([]JSONVerifyResult, len(results))
	for i := range results {
		jsonResults[i] = verifyResultToJSON(&results[i])
	}
	summary := summarizeSignatureResults(results)
	return JSONVerifyOutput{
		OK:             ok,
		Results:        jsonResults,
		Checked:        summary.checked,
		Valid:          summary.valid,
		Unsigned:       summary.unsigned,
		Invalid:        summary.invalid,
		RequireSigned:  requireSigned,
		AllowedSigners: strings.TrimSpace(allowedSignersPath) != "",
	}
}

type signatureSummary struct {
	checked  int
	valid    int
	unsigned int
	invalid  int
}

func summarizeSignatureResults(results []repo.VerificationResult) signatureSummary {
	summary := signatureSummary{checked: len(results)}
	for _, result := range results {
		switch {
		case result.Valid:
			summary.valid++
		case result.Unsigned:
			summary.unsigned++
		default:
			summary.invalid++
		}
	}
	return summary
}

func signatureResultsOK(results []repo.VerificationResult, requireSigned bool) bool {
	for _, result := range results {
		if result.Valid {
			continue
		}
		if result.Unsigned && !requireSigned {
			continue
		}
		return false
	}
	return true
}

func signatureVerificationError(results []repo.VerificationResult, requireSigned bool) error {
	summary := summarizeSignatureResults(results)
	var problems []string
	if summary.invalid > 0 {
		problems = append(problems, fmt.Sprintf("%d invalid or untrusted signature(s)", summary.invalid))
	}
	if requireSigned && summary.unsigned > 0 {
		problems = append(problems, fmt.Sprintf("%d unsigned commit(s)", summary.unsigned))
	}
	if len(problems) == 0 {
		return fmt.Errorf("verify signatures: signature policy failed")
	}
	return fmt.Errorf("verify signatures: %s", strings.Join(problems, ", "))
}

func verifyReportToJSON(report *repo.RepositoryIntegrityReport) JSONVerifyOutput {
	out := JSONVerifyOutput{
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

func repositoryDiagnosticToJSON(d repo.RepositoryDiagnostic) JSONRepositoryDiagnostic {
	return JSONRepositoryDiagnostic{
		Severity:  string(d.Severity),
		Code:      d.Code,
		Message:   d.Message,
		Path:      d.Path,
		Ref:       d.Ref,
		Object:    d.Object,
		Repair:    d.Repair,
		Operation: d.Operation,
	}
}

func firstVerifyError(report *repo.RepositoryIntegrityReport) error {
	for _, d := range report.Diagnostics {
		if d.Severity == repo.DiagnosticError && d.Code == "object_store_invalid" {
			return fmt.Errorf("%s", d.Message)
		}
	}
	for _, d := range report.Diagnostics {
		if d.Severity == repo.DiagnosticError {
			return fmt.Errorf("%s", d.Message)
		}
	}
	return fmt.Errorf("verify: repository has integrity errors")
}

func verifyResultToJSON(result *repo.VerificationResult) JSONVerifyResult {
	return JSONVerifyResult{
		CommitHash: string(result.CommitHash),
		Valid:      result.Valid,
		Unsigned:   result.Unsigned,
		SignerKey:  result.SignerKey,
		Algorithm:  result.Algorithm,
		Error:      result.Error,
	}
}

func printVerificationResult(cmd *cobra.Command, result *repo.VerificationResult) {
	short := string(result.CommitHash)
	if len(short) > 8 {
		short = short[:8]
	}

	if result.Unsigned {
		fmt.Fprintf(cmd.OutOrStdout(), "No signature on commit %s\n", short)
		return
	}

	if result.Valid {
		fmt.Fprintf(cmd.OutOrStdout(), "Good signature (%s) on commit %s\n", result.Algorithm, short)
		return
	}

	fmt.Fprintf(cmd.OutOrStdout(), "BAD signature on commit %s: %s\n", short, result.Error)
}
