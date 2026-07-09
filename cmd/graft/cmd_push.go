package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	var force bool
	var checkOnly bool
	var requireSigned bool
	var allowedSignersPath string

	cmd := &cobra.Command{
		Use:   "push [remote] [branch]",
		Short: "Push a local branch or ref to a remote",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}

			remoteArg := ""
			branch := ""
			switch len(args) {
			case 1:
				candidate := strings.TrimSpace(args[0])
				if looksLikeRemoteURL(candidate) {
					remoteArg = candidate
				} else if _, err := r.RemoteURL(candidate); err == nil {
					remoteArg = candidate
				} else {
					branch = candidate
				}
			case 2:
				remoteArg = strings.TrimSpace(args[0])
				branch = strings.TrimSpace(args[1])
			}
			remoteName, remoteURL, transport, err := resolveRemoteNameAndSpec(r, remoteArg)
			if err != nil {
				return err
			}
			if requireSigned || strings.TrimSpace(allowedSignersPath) != "" {
				_, localRef, _, err := resolvePushRefNames(r, branch)
				if err != nil {
					return err
				}
				if err := verifyPushSignaturePolicy(r, localRef, requireSigned, allowedSignersPath); err != nil {
					return verificationFailureError(err)
				}
			}
			if checkOnly {
				if transport == remoteTransportGit {
					return fmt.Errorf("push --check currently supports orchard/graft remotes only")
				}
				pushTarget, localRef, remoteRef, err := resolvePushRefNames(r, branch)
				if err != nil {
					return err
				}
				report, err := collectPushLimitReport(cmd.Context(), r, pushTarget, localRef, remoteName, remoteURL, remoteRef)
				if err != nil {
					return err
				}
				if err := pushLimitError(report); err != nil {
					return verificationFailureError(err)
				}
				printPushLimitSummary(cmd.OutOrStdout(), report)
				return nil
			}
			if transport == remoteTransportGit {
				return pushViaGit(cmd, r, remoteURL, branch, force)
			}
			return pushBranchGot(cmd, r, remoteName, remoteURL, branch, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "allow non-fast-forward update")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "validate push object limits without uploading anything")
	cmd.Flags().BoolVar(&requireSigned, "require-signed", false, "fail before push if the branch or tag being pushed is unsigned or invalid")
	cmd.Flags().StringVar(&allowedSignersPath, "allowed-signers", "", "OpenSSH allowed_signers file for trusted pushed commit or tag keys")
	return cmd
}

func verifyPushSignaturePolicy(r *repo.Repo, localRef string, requireSigned bool, allowedSignersPath string) error {
	switch {
	case strings.HasPrefix(localRef, "refs/heads/"):
		results, err := collectSignatureResultsFromRef(r, localRef, 100, allowedSignersPath)
		if err != nil {
			return err
		}
		if !signatureResultsOK(results, requireSigned) {
			return signatureVerificationError(results, requireSigned)
		}
		return nil
	case strings.HasPrefix(localRef, "refs/tags/"):
		name := strings.TrimPrefix(localRef, "refs/tags/")
		result, err := verifyTagSignatureForCLI(r, name, allowedSignersPath)
		if err != nil {
			return err
		}
		if !tagVerificationOK(result, requireSigned) {
			return tagVerificationError(result, requireSigned)
		}
		return nil
	default:
		return fmt.Errorf("signature policy supports refs/heads/* and refs/tags/* only")
	}
}

func pushBranchGot(cmd *cobra.Command, r *repo.Repo, remoteName, remoteURL, branch string, force bool) error {
	pushTarget, localRef, remoteRef, err := resolvePushRefNames(r, branch)
	if err != nil {
		return err
	}
	localHash, err := r.ResolveRef(localRef)
	if err != nil {
		return fmt.Errorf("resolve local ref %q: %w", localRef, err)
	}

	client, err := remote.NewClient(remoteURL)
	if err != nil {
		return err
	}
	remoteRefs, err := client.ListRefs(cmd.Context())
	if err != nil {
		return err
	}

	remoteHash, hasRemote := remoteRefs[remoteRef]
	if hasRemote && strings.TrimSpace(string(remoteHash)) == "" {
		hasRemote = false
	}

	// Load hooks config and run pre-push hooks.
	hooksCfg, err := r.LoadHooksConfig(nil)
	if err != nil {
		return err
	}
	hookOptions := repo.HookRunOptions{
		Context:       cmd.Context(),
		Stdout:        cmd.OutOrStdout(),
		Stderr:        cmd.ErrOrStderr(),
		WarningWriter: cmd.ErrOrStderr(),
	}
	prePushHooks := hooksCfg.ForPoint("pre-push")
	if len(prePushHooks) > 0 {
		payload, _ := json.Marshal(repo.PrePushPayload{
			Hook:      "pre-push",
			Repo:      r.RootDir,
			Remote:    remoteName,
			RemoteURL: remoteURL,
			Refs: []repo.HookRefUpdate{
				{LocalRef: localRef, RemoteRef: remoteRef, LocalHash: string(localHash), RemoteHash: string(remoteHash)},
			},
		})
		if err := repo.RunHooksForPointWithOptions(cmd.Context(), r.RootDir, prePushHooks, payload, true, hookOptions); err != nil {
			return err
		}
	}

	if hasRemote && remoteHash == localHash {
		_ = r.UpdateRef(remoteTrackingRefName(remoteName, remoteRef), remoteHash)
		fmt.Fprintf(cmd.OutOrStdout(), "everything up-to-date (%s)\n", shortHash(localHash))
		return nil
	}

	if hasRemote && !force {
		if strings.HasPrefix(remoteRef, "heads/") {
			if !r.Store.Has(remoteHash) {
				haves, err := localRefTips(r)
				if err != nil {
					return err
				}
				if _, err := remote.FetchIntoStore(cmd.Context(), client, r.Store, []object.Hash{remoteHash}, haves); err != nil {
					return fmt.Errorf("push safety check failed fetching remote head: %w", err)
				}
			}
			base, err := r.FindMergeBase(localHash, remoteHash)
			if err != nil {
				return fmt.Errorf("push safety check failed: %w", err)
			}
			if base != remoteHash {
				return fmt.Errorf("push rejected: non-fast-forward (local %s does not contain remote %s)", shortHash(localHash), shortHash(remoteHash))
			}
		} else if remoteHash != localHash {
			return fmt.Errorf("push rejected: remote %s already exists at %s (use --force to overwrite)", remoteRef, shortHash(remoteHash))
		}
	}

	stopRoots := make([]object.Hash, 0, len(remoteRefs))
	for _, h := range remoteRefs {
		if strings.TrimSpace(string(h)) == "" {
			continue
		}
		if r.Store.Has(h) {
			stopRoots = append(stopRoots, h)
		}
	}

	objectsToPush, err := remote.CollectObjectsForPush(r.Store, []object.Hash{localHash}, stopRoots)
	if err != nil {
		return err
	}
	uploaded, err := pushObjectsChunked(cmd.Context(), client, objectsToPush)
	if err != nil {
		return err
	}

	old := object.Hash("")
	if hasRemote {
		old = remoteHash
	}
	newHash := localHash
	updated, err := client.UpdateRefs(cmd.Context(), []remote.RefUpdate{{
		Name: remoteRef,
		Old:  &old,
		New:  &newHash,
	}})
	if err != nil {
		return err
	}

	finalHash := localHash
	if h, ok := updated[remoteRef]; ok && strings.TrimSpace(string(h)) != "" {
		finalHash = h
	}
	if err := r.UpdateRef(remoteTrackingRefName(remoteName, remoteRef), finalHash); err != nil {
		return err
	}

	if hasRemote {
		fmt.Fprintf(cmd.OutOrStdout(), "pushed %s: %s -> %s (%d objects)\n", pushTarget, shortHash(remoteHash), shortHash(finalHash), uploaded)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "pushed new %s at %s (%d objects)\n", pushTarget, shortHash(finalHash), uploaded)
	}

	// Run post-push hooks (non-blocking: errors are warnings only).
	postPushHooks := hooksCfg.ForPoint("post-push")
	if len(postPushHooks) > 0 {
		payload, _ := json.Marshal(repo.PostPushPayload{
			Hook:          "post-push",
			Remote:        remoteName,
			RemoteURL:     remoteURL,
			Refs:          []repo.HookRefUpdate{{Name: remoteRef, Old: string(remoteHash), New: string(finalHash)}},
			ObjectsPushed: uploaded,
		})
		_ = repo.RunHooksForPointWithOptions(cmd.Context(), r.RootDir, postPushHooks, payload, false, hookOptions)
	}

	// Push LFS objects referenced by the commit.
	lfsClient := remote.NewLFSClient(client)
	lfsCount, err := r.PushLFSObjects(cmd.Context(), lfsClient, localHash)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: LFS push failed: %v\n", err)
	} else if lfsCount > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "pushed %d LFS objects\n", lfsCount)
	}

	return nil
}

func resolvePushRefNames(r *repo.Repo, branchArg string) (display string, localRef string, remoteRef string, err error) {
	branchArg = strings.TrimSpace(branchArg)
	if branchArg == "" {
		branchArg, err = r.CurrentBranch()
		if err != nil {
			return "", "", "", err
		}
		if branchArg == "" {
			return "", "", "", fmt.Errorf("cannot infer branch while HEAD is detached; specify branch or full ref")
		}
	}

	if strings.HasPrefix(branchArg, "refs/heads/") {
		name := strings.TrimPrefix(branchArg, "refs/heads/")
		if strings.TrimSpace(name) == "" {
			return "", "", "", fmt.Errorf("invalid branch ref %q", branchArg)
		}
		return "branch " + name, branchArg, "heads/" + name, nil
	}
	if strings.HasPrefix(branchArg, "refs/tags/") {
		name := strings.TrimPrefix(branchArg, "refs/tags/")
		if strings.TrimSpace(name) == "" {
			return "", "", "", fmt.Errorf("invalid tag ref %q", branchArg)
		}
		return "tag " + name, branchArg, "tags/" + name, nil
	}
	if strings.HasPrefix(branchArg, "refs/") {
		return "", "", "", fmt.Errorf("unsupported ref %q (only refs/heads/* and refs/tags/* are supported)", branchArg)
	}
	// Check if the bare name matches a tag before defaulting to branch.
	if r != nil {
		if _, tagErr := r.ResolveRef("refs/tags/" + branchArg); tagErr == nil {
			return "tag " + branchArg, "refs/tags/" + branchArg, "tags/" + branchArg, nil
		}
	}
	return "branch " + branchArg, "refs/heads/" + branchArg, "heads/" + branchArg, nil
}

func pushObjectsChunked(ctx context.Context, client *remote.Client, objects []remote.ObjectRecord) (int, error) {
	if len(objects) == 0 {
		return 0, nil
	}

	limits := effectivePushChunkLimits(client)
	chunk := make([]remote.ObjectRecord, 0, limits.objectCount)
	chunkBytes := 0
	uploaded := 0
	usePack := shouldUsePackPush(client)
	useResumablePack := shouldUseResumablePackPush(client)

	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		if usePack {
			if useResumablePack {
				if _, err := client.PushObjectsPackResumable(ctx, chunk, remote.ResumablePackUploadOptions{}); err != nil {
					return err
				}
				uploaded += len(chunk)
				chunk = chunk[:0]
				chunkBytes = 0
				return nil
			}
			if err := client.PushObjectsPack(ctx, chunk); err != nil {
				if !remote.IsPackUploadUnsupported(err) {
					return err
				}
				usePack = false
			} else {
				uploaded += len(chunk)
				chunk = chunk[:0]
				chunkBytes = 0
				return nil
			}
		}
		if err := client.PushObjects(ctx, chunk); err != nil {
			return err
		}
		uploaded += len(chunk)
		chunk = chunk[:0]
		chunkBytes = 0
		return nil
	}

	for _, obj := range objects {
		if len(obj.Data) > limits.objectBytes {
			return uploaded, fmt.Errorf("object %s exceeds %d-byte push limit", shortHash(obj.Hash), limits.objectBytes)
		}
		recBytes := len(obj.Data) + 128
		if len(chunk) > 0 && (len(chunk) >= limits.objectCount || chunkBytes+recBytes > limits.payloadBytes) {
			if err := flush(); err != nil {
				return uploaded, err
			}
		}
		chunk = append(chunk, obj)
		chunkBytes += recBytes
	}
	if err := flush(); err != nil {
		return uploaded, err
	}
	return uploaded, nil
}

type pushChunkLimits struct {
	objectCount  int
	payloadBytes int
	objectBytes  int
}

func effectivePushChunkLimits(client *remote.Client) pushChunkLimits {
	limits := pushChunkLimits{
		objectCount:  pushChunkObjectLimit,
		payloadBytes: pushChunkByteLimit,
		objectBytes:  pushObjectByteLimit,
	}
	if client == nil {
		return limits
	}
	serverLimits := client.ServerLimits()
	if serverLimits == nil {
		return limits
	}
	if serverLimits.MaxBatch > 0 && serverLimits.MaxBatch < limits.objectCount {
		limits.objectCount = serverLimits.MaxBatch
	}
	if serverLimits.MaxPayload > 0 && serverLimits.MaxPayload < limits.payloadBytes {
		limits.payloadBytes = serverLimits.MaxPayload
	}
	if serverLimits.MaxObject > 0 && serverLimits.MaxObject < limits.objectBytes {
		limits.objectBytes = serverLimits.MaxObject
	}
	return limits
}

func shouldUsePackPush(client *remote.Client) bool {
	if client == nil {
		return false
	}
	caps := client.ServerCapabilities()
	if caps == nil {
		return true
	}
	return caps.Has(remote.CapPack) && caps.Has(remote.CapZstd)
}

func shouldUseResumablePackPush(client *remote.Client) bool {
	if client == nil {
		return false
	}
	caps := client.ServerCapabilities()
	return caps != nil && caps.Has(remote.CapPack) && caps.Has(remote.CapZstd) && caps.Has(remote.CapResumablePack)
}
