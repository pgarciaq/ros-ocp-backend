package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// manGeneratedAt is pinned so generated pages are byte-deterministic:
// cobra/doc defaults header.Date to time.Now(), which would make every
// regeneration differ in the .TH line and defeat the make man-check
// drift guard. Bump it when the CLI surface changes meaningfully
// (it only feeds the manpage header, not content).
var manGeneratedAt = time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)

// generateDocs writes roff manpages for the public command tree into
// manOut and a markdown tree into mdOut (skipped when mdOut is empty).
// It always generates from a fresh newRootCmd() so the hidden man
// command never emits a page for itself. Both formats are deterministic
// for a given source tree, which is what make man-check relies on.
func generateDocs(manOut, mdOut string) error {
	root := newRootCmd()

	if manOut != "" {
		if err := os.MkdirAll(manOut, 0o750); err != nil {
			return fmt.Errorf("creating manpage output dir: %w", err)
		}
		header := &doc.GenManHeader{
			Title:   "ROBNE",
			Section: "1",
			Source:  fmt.Sprintf("ros-ocp-backend %s", version),
			Manual:  "robne CLI",
			Date:    &manGeneratedAt,
		}
		if err := doc.GenManTree(root, header, manOut); err != nil {
			return fmt.Errorf("generating manpages: %w", err)
		}
	}

	if mdOut != "" {
		if err := os.MkdirAll(mdOut, 0o750); err != nil {
			return fmt.Errorf("creating markdown output dir: %w", err)
		}
		if err := doc.GenMarkdownTree(root, mdOut); err != nil {
			return fmt.Errorf("generating markdown docs: %w", err)
		}
	}

	return nil
}

// newManCmd is the hidden generator behind `make man` (#703). Hidden so
// the public surface stays five subcommands and `--help` stays clean.
func newManCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "man",
		Short: "Generate robne manpages and markdown docs from the command tree",
		Long: `Generate roff manpages (robne.1, robne-recommend.1, ...) and a markdown
tree from the live cobra command tree, so the pages cannot drift from
--help. Used by ` + "`make man`" + `; output is committed under docs/man and
checked in CI with ` + "`make man-check`" + `.

Preview without installing: man -M docs/man robne
(macOS and Linux; Windows users should read docs/man/markdown or --help).`,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manOut, _ := cmd.Flags().GetString("out")
			mdOut, _ := cmd.Flags().GetString("markdown-out")
			if err := generateDocs(manOut, mdOut); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(w, "wrote manpages to %s\n", manOut); err != nil {
				return err
			}
			if mdOut != "" {
				if _, err := fmt.Fprintf(w, "wrote markdown docs to %s\n", mdOut); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().String("out", filepath.Join("docs", "man", "man1"),
		"directory for roff manpages (empty skips roff)")
	cmd.Flags().String("markdown-out", filepath.Join("docs", "man", "markdown"),
		"directory for the markdown tree (empty skips markdown)")
	return cmd
}
