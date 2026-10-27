package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateDocsToTemp(t *testing.T) (manDir, mdDir string) {
	t.Helper()
	manDir = t.TempDir()
	mdDir = t.TempDir()
	require.NoError(t, generateDocs(manDir, mdDir))
	return manDir, mdDir
}

// #703: the generator must not become a sixth public subcommand — the
// public tree stays recommend/validate/diff/explain/version.
func TestManCmdHiddenFromPublicTree(t *testing.T) {
	root := newRootCmd()
	for _, c := range root.Commands() {
		assert.NotEqual(t, "man", c.Name(), "man must not hang off the public root")
	}
	assert.True(t, newManCmd().Hidden, "the man command must be hidden")
}

// #703 close criterion: pages for all five public subcommands plus the
// root, and no page for the hidden generator itself.
func TestManGeneratesAllSubcommands(t *testing.T) {
	manDir, _ := generateDocsToTemp(t)
	for _, name := range []string{
		"robne.1", "robne-recommend.1", "robne-validate.1",
		"robne-diff.1", "robne-explain.1", "robne-version.1",
	} {
		assert.FileExists(t, filepath.Join(manDir, name), "expected manpage %s", name)
	}
	assert.NoFileExists(t, filepath.Join(manDir, "robne-man.1"),
		"the generator must not emit a page for itself")
}

// The markdown tree is the Windows/editor-friendly companion rendered
// from the same command tree.
func TestMarkdownGeneratesAllSubcommands(t *testing.T) {
	_, mdDir := generateDocsToTemp(t)
	for _, name := range []string{
		"robne.md", "robne_recommend.md", "robne_validate.md",
		"robne_diff.md", "robne_explain.md", "robne_version.md",
	} {
		assert.FileExists(t, filepath.Join(mdDir, name), "expected markdown page %s", name)
	}
	assert.NoFileExists(t, filepath.Join(mdDir, "robne_man.md"))
}

// The make man-check CI guard diffs regenerated pages against the
// committed ones; time-of-generation must not leak into the bytes.
func TestGeneratedDocsDeterministic(t *testing.T) {
	manA, mdA := generateDocsToTemp(t)
	manB, mdB := generateDocsToTemp(t)
	for _, name := range []string{"robne.1", "robne-recommend.1", "robne-explain.1"} {
		a, err := os.ReadFile(filepath.Join(manA, name)) //nolint:gosec // G304: t.TempDir output
		require.NoError(t, err)
		b, err := os.ReadFile(filepath.Join(manB, name)) //nolint:gosec // G304: t.TempDir output
		require.NoError(t, err)
		assert.Equal(t, string(a), string(b), "%s must be byte-identical across runs", name)
	}
	a, err := os.ReadFile(filepath.Join(mdA, "robne_explain.md")) //nolint:gosec // G304: t.TempDir output
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(mdB, "robne_explain.md")) //nolint:gosec // G304: t.TempDir output
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b), "markdown must be byte-identical across runs")
}

// #703 names the #700 hcp surface as a must-cover: the explain pages
// carry --scenario and --observed-at. This is also the tripwire that the
// flags are actually registered on the cobra command (unregistered flags
// do not appear in generated docs).
func TestGeneratedDocsCoverHCPFlags(t *testing.T) {
	_, mdDir := generateDocsToTemp(t)
	explain, err := os.ReadFile(filepath.Join(mdDir, "robne_explain.md")) //nolint:gosec // G304: t.TempDir output
	require.NoError(t, err)
	assert.Contains(t, string(explain), "--scenario",
		"explain page must cover the hcp scenario selector")
	assert.Contains(t, string(explain), "--observed-at")
}
