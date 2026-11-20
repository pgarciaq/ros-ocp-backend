package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
// for a given source tree, which is what make man-check relies on: the
// .1 pages take their date from the pinned GenManHeader below, and the
// markdown pages get the same pinned date rewritten in after generation
// (see pinMarkdownDates) because cobra's markdown writer has no date
// injection point.
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
		if err := pinMarkdownDates(mdOut); err != nil {
			return fmt.Errorf("pinning markdown dates: %w", err)
		}
	}

	if err := appendManSections(manOut, mdOut); err != nil {
		return fmt.Errorf("appending manpage sections: %w", err)
	}

	return nil
}

// #908: identity sections in GNU ordering (BUGS, COPYRIGHT, SEE ALWAYS last).
// cobra's generators emit none of them, so every generated page — roff and
// markdown — gains the three sections in a post-generation pass. SEE ALSO
// lists the sibling pages (robne(1) for the root, robne-<cmd>(1) for
// subcommands) plus the documentation site. The copyright year is a static
// string by owner decision — bumped by hand like a LICENSE header.
const (
	manIssuesURL = "https://github.com/pgarciaq/ros-ocp-backend/issues"
	manDocsURL   = "https://pgarciaq.github.io/ros-ocp-backend/features/robne-cli/"
	manCopyright = "(c) Copyright 2026 Red Hat"
)

// manPageRef maps a generated page file stem to its manpage reference:
// "robne" -> "robne(1)", "robne-recommend" -> "robne-recommend(1)".
func manPageRef(stem string) string {
	return stem + "(1)"
}

// seeAlsoList builds the comma-separated sibling references for one page.
func seeAlsoList(pages []string, self string) string {
	refs := make([]string, 0, len(pages)-1)
	for _, p := range pages {
		if p != self {
			refs = append(refs, manPageRef(p))
		}
	}
	return strings.Join(refs, ", ")
}

func appendManSections(manOut, mdOut string) error {
	// The same logical pages live in both output dirs — collect a
	// deduplicated set or every file would gain the sections twice.
	set := map[string]bool{}
	if manOut != "" {
		entries, err := os.ReadDir(manOut)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".1") {
				set[strings.TrimSuffix(e.Name(), ".1")] = true
			}
		}
	}
	if mdOut != "" {
		entries, err := os.ReadDir(mdOut)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				// cobra's markdown names: robne.md, robne_recommend.md
				set[strings.ReplaceAll(strings.TrimSuffix(e.Name(), ".md"), "_", "-")] = true
			}
		}
	}
	pages := make([]string, 0, len(set))
	for p := range set {
		pages = append(pages, p)
	}
	sort.Strings(pages)

	if manOut != "" {
		for _, page := range pages {
			path := filepath.Join(manOut, page+".1")
			section := fmt.Sprintf("\n.SH BUGS\nBugs are tracked in GitHub Issues:\n%s\n.SH COPYRIGHT\n%s\n.SH SEE ALSO\n%s\nFull documentation:\n%s\n",
				manIssuesURL, manCopyright, seeAlsoList(pages, page), manDocsURL)
			// cobra emits its own trailing SEE ALSO (the parent reference);
			// replace it — the enriched one below carries that reference
			// among the siblings.
			if err := replaceTrailingSection(path, "\n.SH SEE ALSO\n", []byte(section)); err != nil {
				return err
			}
		}
	}
	if mdOut != "" {
		for _, page := range pages {
			fileName := strings.ReplaceAll(page, "-", "_") + ".md"
			path := filepath.Join(mdOut, fileName)
			section := fmt.Sprintf("\n## BUGS\n\nBugs are tracked in GitHub Issues:\n%s\n\n## COPYRIGHT\n\n%s\n\n## SEE ALSO\n\n%s\n\nFull documentation:\n%s\n",
				manIssuesURL, manCopyright, seeAlsoList(pages, page), manDocsURL)
			if err := insertBeforeTrailer(path, "### SEE ALSO", []byte(section)); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendToFile(path string, data []byte) error {
	f, err := os.OpenFile(filepath.Clean(path), //nolint:gosec // G304: generator output dir
		os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-only close on generator output
	_, err = f.Write(data)
	return err
}

// replaceTrailingSection drops everything from marker onward (cobra's own
// trailing SEE ALSO block — its last emitted section) and writes the
// replacement section in its place. Absent marker: plain append.
func replaceTrailingSection(path string, marker string, section []byte) error {
	b, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // G304: generator output dir
	if err != nil {
		return err
	}
	i := bytes.LastIndex(b, []byte(marker))
	if i < 0 {
		return appendToFile(path, section)
	}
	updated := append(append([]byte{}, b[:i]...), section...)
	return os.WriteFile(path, updated, 0o640) //nolint:gosec // G306: generator output
}

// insertBeforeTrailer places the section block ahead of cobra's
// "###### Auto generated by" footer so that trailer stays the last line,
// dropping cobra's own trailing "### SEE ALSO" block when present.
func insertBeforeTrailer(path string, cobraMarker string, section []byte) error {
	b, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // G304: generator output dir
	if err != nil {
		return err
	}
	start := bytes.LastIndex(b, []byte(cobraMarker))
	t := bytes.LastIndex(b, []byte(mdFooterPrefix))
	if t < 0 {
		if start < 0 {
			return appendToFile(path, section)
		}
		updated := append(append([]byte{}, b[:start]...), section...)
		return os.WriteFile(path, updated, 0o640) //nolint:gosec // G306: generator output
	}
	if start < 0 || start > t {
		start = t
	}
	updated := append(append(append([]byte{}, b[:start]...), section...), b[t:]...)
	return os.WriteFile(path, updated, 0o640) //nolint:gosec // G306: generator output
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

// mdFooterPrefix is the trailer cobra appends to every markdown page.
const mdFooterPrefix = "###### Auto generated by spf13/cobra on "

// pinMarkdownDates rewrites that trailer's date to manGeneratedAt. cobra's
// markdown writer formats time.Now() straight into the page with no injection
// point (unlike GenManHeader.Date for the .1 pages), so without this pass every
// regeneration differs by the day it ran and make man-check can never be green
// across days. Only the trailer's date is touched; page bodies are left alone.
func pinMarkdownDates(dir string) error {
	stamp := mdFooterPrefix + manGeneratedAt.Format("2-Jan-2006")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // G304: generator output dir
		if err != nil {
			return err
		}
		i := bytes.LastIndex(b, []byte(mdFooterPrefix))
		if i < 0 {
			continue // no trailer to pin
		}
		end := bytes.IndexByte(b[i:], '\n')
		if end < 0 {
			end = len(b) - i
		}
		updated := append(append(append([]byte{}, b[:i]...), stamp...), b[i+end:]...)
		if bytes.Equal(b, updated) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Clean(path), updated, info.Mode().Perm()); err != nil { //nolint:gosec // G703: path is a .md entry from the generator-owned output dir
			return err
		}
	}
	return nil
}
