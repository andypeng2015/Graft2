package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func newManCmd(root *cobra.Command) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "man",
		Short: "Generate man pages",
		Long:  "Generate section 1 man pages for graft and all available subcommands.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := root
			if target == nil {
				target = cmd.Root()
			}
			outDir := filepath.Clean(dir)
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("create man page directory %q: %w", outDir, err)
			}
			header := &doc.GenManHeader{
				Title:   "GRAFT",
				Section: "1",
				Source:  "graft " + version,
				Manual:  "Graft Manual",
			}
			if err := doc.GenManTree(target, header, outDir); err != nil {
				return fmt.Errorf("generate man pages: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "generated man pages in %s\n", outDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "man", "directory to write generated man pages")
	return cmd
}
