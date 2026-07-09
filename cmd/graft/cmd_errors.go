package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/odvcencio/graft/pkg/redact"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

const (
	exitSuccess               = 0
	exitGeneralFailure        = 1
	exitUsageError            = 2
	exitConflict              = 3
	exitVerificationFailure   = 4
	exitAuthenticationFailure = 5
	exitNetworkFailure        = 6
	exitRepositoryNeedsRepair = 7
)

const (
	errorCodeUsage                 = "usage"
	errorCodeConflict              = "conflict"
	errorCodeVerificationFailed    = "verification_failed"
	errorCodeAuthenticationFailed  = "auth_failed"
	errorCodeNetworkFailed         = "network_failed"
	errorCodeRepositoryNeedsRepair = "repository_needs_repair"
)

type commandError struct {
	code       string
	exitCode   int
	suggestion string
	err        error
}

func newCommandError(code string, exitCode int, err error, suggestion string) error {
	if err == nil {
		return nil
	}
	return &commandError{
		code:       code,
		exitCode:   exitCode,
		suggestion: strings.TrimSpace(suggestion),
		err:        err,
	}
}

func usageError(cmd *cobra.Command, err error) error {
	suggestion := "run `graft --help`"
	if cmd != nil && strings.TrimSpace(cmd.CommandPath()) != "" {
		suggestion = fmt.Sprintf("run `%s --help`", cmd.CommandPath())
	}
	return newCommandError(errorCodeUsage, exitUsageError, err, suggestion)
}

func conflictError(err error, suggestion string) error {
	return newCommandError(errorCodeConflict, exitConflict, err, suggestion)
}

func verificationFailureError(err error) error {
	return newCommandError(errorCodeVerificationFailed, exitVerificationFailure, err, "run `graft doctor` for repair guidance")
}

func repositoryNeedsRepairError(err error) error {
	return newCommandError(errorCodeRepositoryNeedsRepair, exitRepositoryNeedsRepair, err, "run `graft doctor --json` or the suggested `graft repair ...` command")
}

func (e *commandError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *commandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *commandError) ExitCode() int {
	if e == nil || e.exitCode <= 0 {
		return exitGeneralFailure
	}
	return e.exitCode
}

func (e *commandError) Code() string {
	if e == nil {
		return ""
	}
	return e.code
}

func (e *commandError) Suggestion() string {
	if e == nil {
		return ""
	}
	return e.suggestion
}

func commandExitCode(err error) int {
	if err == nil {
		return exitSuccess
	}
	var cmdErr *commandError
	if errors.As(err, &cmdErr) {
		return cmdErr.ExitCode()
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	if classified := classifyCommandError(err); classified != nil {
		return classified.ExitCode()
	}
	return exitGeneralFailure
}

func printCommandError(w io.Writer, err error) {
	if err == nil {
		return
	}
	message := redact.Text(err.Error())
	if classified := commandErrorForDisplay(err); classified != nil {
		fmt.Fprintf(w, "error [%s]: %s\n", classified.Code(), message)
		if suggestion := classified.Suggestion(); suggestion != "" {
			fmt.Fprintf(w, "suggestion: %s\n", redact.Text(suggestion))
		}
		return
	}
	fmt.Fprintln(w, message)
}

func commandErrorForDisplay(err error) *commandError {
	var cmdErr *commandError
	if errors.As(err, &cmdErr) {
		return cmdErr
	}
	return classifyCommandError(err)
}

func classifyCommandError(err error) *commandError {
	if err == nil {
		return nil
	}
	var rebaseConflict *repo.ErrRebaseConflict
	var cherryPickConflict *repo.ErrCherryPickConflict
	var revertConflict *repo.ErrRevertConflict
	if errors.Is(err, errMergeConflict) ||
		errors.As(err, &rebaseConflict) ||
		errors.As(err, &cherryPickConflict) ||
		errors.As(err, &revertConflict) {
		return &commandError{
			code:       errorCodeConflict,
			exitCode:   exitConflict,
			suggestion: "resolve conflicts, then run the matching --continue command or `graft commit`",
			err:        err,
		}
	}

	var remoteErr *remote.RemoteError
	if errors.As(err, &remoteErr) {
		if remoteErrorLooksAuth(remoteErr) {
			return &commandError{
				code:       errorCodeAuthenticationFailed,
				exitCode:   exitAuthenticationFailure,
				suggestion: "run `graft auth status` or refresh credentials with `graft auth setup`",
				err:        err,
			}
		}
		return &commandError{
			code:       errorCodeNetworkFailed,
			exitCode:   exitNetworkFailure,
			suggestion: "retry the remote operation; run `graft doctor --bundle` if it persists",
			err:        err,
		}
	}

	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) ||
		errors.Is(err, remote.ErrRemoteResponseTooLarge) ||
		errors.Is(err, remote.ErrRemotePaginationLimitExceeded) {
		return &commandError{
			code:       errorCodeNetworkFailed,
			exitCode:   exitNetworkFailure,
			suggestion: "retry the remote operation; run `graft doctor --bundle` if it persists",
			err:        err,
		}
	}

	msg := strings.ToLower(err.Error())
	switch {
	case looksLikeUsageError(msg):
		return &commandError{
			code:       errorCodeUsage,
			exitCode:   exitUsageError,
			suggestion: "run `graft --help` or `<command> --help`",
			err:        err,
		}
	case strings.Contains(msg, "repository has integrity errors"):
		return &commandError{
			code:       errorCodeVerificationFailed,
			exitCode:   exitVerificationFailure,
			suggestion: "run `graft doctor` for repair guidance",
			err:        err,
		}
	case strings.Contains(msg, "needs_repair") || strings.Contains(msg, "needs repair") || strings.Contains(msg, "repair transaction"):
		return &commandError{
			code:       errorCodeRepositoryNeedsRepair,
			exitCode:   exitRepositoryNeedsRepair,
			suggestion: "run `graft doctor --json` or the suggested `graft repair ...` command",
			err:        err,
		}
	}
	return nil
}

func remoteErrorLooksAuth(err *remote.RemoteError) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Code + " " + err.Message + " " + err.Detail)
	return strings.Contains(text, "auth") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "forbidden") ||
		strings.Contains(text, "permission") ||
		strings.Contains(text, "token")
}

func looksLikeUsageError(msg string) bool {
	return strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "accepts ") ||
		strings.Contains(msg, "requires at least") ||
		strings.Contains(msg, "requires at most") ||
		strings.Contains(msg, "required argument") ||
		strings.Contains(msg, "required flag") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "cannot be used with") ||
		strings.Contains(msg, "mutually exclusive")
}
