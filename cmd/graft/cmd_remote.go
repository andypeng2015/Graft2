package main

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newRemoteCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage repository remotes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}
			cfg, err := r.ReadConfig()
			if err != nil {
				return err
			}
			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), remoteConfigToJSON(cfg.Remotes))
			}
			names := make([]string, 0, len(cfg.Remotes))
			for name := range cfg.Remotes {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", name, cfg.Remotes[name])
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	var addAllowInsecure bool
	addCmd := &cobra.Command{
		Use:   "add <name> <url>",
		Short: "Add a named remote",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}
			remoteURL, _, err := parseAnyRemoteSpec(args[1])
			if err != nil {
				return fmt.Errorf("invalid remote URL %q: %w", args[1], err)
			}
			if err := validateRemoteTransportTrust(remoteURL, addAllowInsecure); err != nil {
				return err
			}
			if err := r.SetRemote(args[0], remoteURL); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added remote %q -> %s\n", args[0], remoteURL)
			return nil
		},
	}
	addCmd.Flags().BoolVar(&addAllowInsecure, "allow-insecure", false, "allow non-local HTTP or git:// remote URLs")
	cmd.AddCommand(addCmd)

	var setAllowInsecure bool
	setCmd := &cobra.Command{
		Use:   "set-url <name> <url>",
		Short: "Update a named remote URL",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRepoForCommand(cmd, ".")
			if err != nil {
				return err
			}
			remoteURL, _, err := parseAnyRemoteSpec(args[1])
			if err != nil {
				return fmt.Errorf("invalid remote URL %q: %w", args[1], err)
			}
			if err := validateRemoteTransportTrust(remoteURL, setAllowInsecure); err != nil {
				return err
			}
			if err := r.SetRemote(args[0], remoteURL); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "updated remote %q -> %s\n", args[0], remoteURL)
			return nil
		},
	}
	setCmd.Flags().BoolVar(&setAllowInsecure, "allow-insecure", false, "allow non-local HTTP or git:// remote URLs")
	cmd.AddCommand(setCmd)

	return cmd
}

func remoteConfigToJSON(remotes map[string]string) JSONRemoteOutput {
	names := make([]string, 0, len(remotes))
	for name := range remotes {
		names = append(names, name)
	}
	sort.Strings(names)

	out := JSONRemoteOutput{Remotes: make([]JSONRemoteEntry, 0, len(names))}
	for _, name := range names {
		out.Remotes = append(out.Remotes, remoteEntryToJSON(name, remotes[name]))
	}
	return out
}

func remoteEntryToJSON(name, rawURL string) JSONRemoteEntry {
	entry := JSONRemoteEntry{
		Name:      name,
		URL:       redactSupportURL(rawURL),
		Transport: "unknown",
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		entry.Warning = "remote URL is empty"
		return entry
	}
	kind, canonical, err := parseRemoteSpec(rawURL)
	if err != nil {
		entry.Warning = "remote URL could not be classified"
		return entry
	}
	entry.Transport = string(kind)
	entry.URL = redactSupportURL(canonical)
	if warning := remoteTransportTrustWarning(canonical); warning != "" {
		entry.Warning = warning
	}
	return entry
}

func validateRemoteTransportTrust(remoteURL string, allowInsecure bool) error {
	if allowInsecure {
		return nil
	}
	if warning := remoteTransportTrustWarning(remoteURL); warning != "" {
		return fmt.Errorf("%s (pass --allow-insecure to store this remote anyway)", warning)
	}
	return nil
}

func remoteTransportTrustWarning(remoteURL string) string {
	u, err := url.Parse(strings.TrimSpace(remoteURL))
	if err != nil || u.Scheme == "" {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "ssh", "file":
		return ""
	case "http":
		if isLocalRemoteHost(u.Hostname()) {
			return ""
		}
		return fmt.Sprintf("remote URL %s uses insecure HTTP; use HTTPS for production remotes", redactSupportURL(remoteURL))
	case "git":
		return fmt.Sprintf("remote URL %s uses unauthenticated git:// transport; use HTTPS or SSH for production remotes", redactSupportURL(remoteURL))
	default:
		return ""
	}
}

func isLocalRemoteHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
