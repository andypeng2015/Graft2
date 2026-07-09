package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newTagCmd() *cobra.Command {
	var deleteTag string
	var force bool
	var showHash bool
	var annotate bool
	var message string
	var tagger string
	var sign bool
	var signKey string
	var verifyTag string
	var allowedSignersPath string
	var requireSigned bool
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "tag [name] [target]",
		Short: "List, create, or delete tags",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}

			if strings.TrimSpace(verifyTag) != "" {
				if len(args) > 0 {
					return fmt.Errorf("tag --verify does not accept positional args")
				}
				result, err := verifyTagSignatureForCLI(r, verifyTag, allowedSignersPath)
				if err != nil {
					return err
				}
				ok := tagVerificationOK(result, requireSigned)
				if jsonFlag {
					if err := writeJSON(cmd.OutOrStdout(), tagVerificationToJSON(result, ok, requireSigned, allowedSignersPath)); err != nil {
						return err
					}
					if !ok {
						return verificationFailureError(tagVerificationError(result, requireSigned))
					}
					return nil
				}
				printTagVerificationResult(cmd, result)
				if !ok {
					return verificationFailureError(tagVerificationError(result, requireSigned))
				}
				return nil
			}

			if strings.TrimSpace(deleteTag) != "" {
				if len(args) > 0 {
					return fmt.Errorf("tag --delete does not accept positional args")
				}
				return r.DeleteTag(deleteTag)
			}

			if len(args) == 0 {
				tags, err := r.ListTagsWithHashes()
				if err != nil {
					return err
				}
				names := make([]string, 0, len(tags))
				for name := range tags {
					names = append(names, name)
				}
				sort.Strings(names)

				for _, name := range names {
					if showHash {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", tags[name], name)
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), name)
					}
				}
				return nil
			}

			name := args[0]
			var target object.Hash
			if len(args) == 2 {
				targetArg := strings.TrimSpace(args[1])
				if resolved, err := r.ResolveRef(targetArg); err == nil {
					target = resolved
				} else {
					target = object.Hash(targetArg)
				}
			} else {
				head, err := r.ResolveRef("HEAD")
				if err != nil {
					return fmt.Errorf("resolve HEAD: %w", err)
				}
				target = head
			}

			if strings.TrimSpace(message) != "" {
				annotate = true
			}
			if strings.TrimSpace(signKey) != "" {
				sign = true
			}
			if sign {
				annotate = true
			}
			if annotate {
				if strings.TrimSpace(message) == "" {
					return fmt.Errorf("annotated tags require --message")
				}
				tagIdentity := strings.TrimSpace(tagger)
				if tagIdentity == "" {
					tagIdentity = r.ResolveAuthor()
				}
				var signer repo.CommitSigner
				if sign {
					resolvedKey := strings.TrimSpace(signKey)
					if resolvedKey == "" {
						if cfg := loadUserConfig(); cfg != nil {
							resolvedKey = strings.TrimSpace(cfg.SigningKeyPath)
						}
					}
					var signErr error
					signer, _, signErr = newSSHCommitSigner(resolvedKey)
					if signErr != nil {
						return signErr
					}
				}
				_, err := r.CreateAnnotatedTagWithSigner(name, target, tagIdentity, message, force, signer)
				return err
			}
			return r.CreateTag(name, target, force)
		},
	}

	cmd.Flags().StringVarP(&deleteTag, "delete", "d", "", "delete the named tag")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace an existing tag")
	cmd.Flags().BoolVar(&showHash, "show-hash", false, "show tag target hashes when listing")
	cmd.Flags().BoolVarP(&annotate, "annotate", "a", false, "create an annotated tag object")
	cmd.Flags().StringVarP(&message, "message", "m", "", "tag message (implies --annotate)")
	cmd.Flags().StringVar(&tagger, "tagger", "", "override tagger identity (default: $USER)")
	cmd.Flags().BoolVar(&sign, "sign", false, "sign annotated tag with SSH private key")
	cmd.Flags().StringVar(&signKey, "sign-key", "", "path to SSH private key for tag signing")
	cmd.Flags().StringVar(&verifyTag, "verify", "", "verify the named tag signature")
	cmd.Flags().StringVar(&allowedSignersPath, "allowed-signers", "", "OpenSSH allowed_signers file for trusted tag keys")
	cmd.Flags().BoolVar(&requireSigned, "require-signed", false, "fail if the tag is unsigned or has an invalid signature")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	return cmd
}

func verifyTagSignatureForCLI(r *repo.Repo, name string, allowedSignersPath string) (*repo.TagVerificationResult, error) {
	if strings.TrimSpace(allowedSignersPath) == "" {
		return r.VerifyTagSignature(name)
	}
	signers, err := repo.LoadAllowedSigners(allowedSignersPath)
	if err != nil {
		return nil, err
	}
	return r.VerifyTagAgainstAllowedSigners(name, signers)
}

func tagVerificationOK(result *repo.TagVerificationResult, requireSigned bool) bool {
	if result.Valid {
		return true
	}
	return result.Unsigned && !requireSigned
}

func tagVerificationError(result *repo.TagVerificationResult, requireSigned bool) error {
	if result.Unsigned && requireSigned {
		return fmt.Errorf("verify tag %s: unsigned tag", result.TagName)
	}
	if result.Error != "" {
		return fmt.Errorf("verify tag %s: %s", result.TagName, result.Error)
	}
	return fmt.Errorf("verify tag %s: signature policy failed", result.TagName)
}

func tagVerificationToJSON(result *repo.TagVerificationResult, ok bool, requireSigned bool, allowedSignersPath string) JSONTagVerifyOutput {
	return JSONTagVerifyOutput{
		OK:             ok,
		TagName:        result.TagName,
		TagHash:        string(result.TagHash),
		TargetHash:     string(result.TargetHash),
		Valid:          result.Valid,
		Unsigned:       result.Unsigned,
		SignerKey:      result.SignerKey,
		Algorithm:      result.Algorithm,
		Error:          result.Error,
		RequireSigned:  requireSigned,
		AllowedSigners: strings.TrimSpace(allowedSignersPath) != "",
	}
}

func printTagVerificationResult(cmd *cobra.Command, result *repo.TagVerificationResult) {
	if result.Unsigned {
		fmt.Fprintf(cmd.OutOrStdout(), "No signature on tag %s\n", result.TagName)
		return
	}
	if result.Valid {
		fmt.Fprintf(cmd.OutOrStdout(), "Good signature (%s) on tag %s\n", result.Algorithm, result.TagName)
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "BAD signature on tag %s: %s\n", result.TagName, result.Error)
}
