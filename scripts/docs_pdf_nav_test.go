package scripts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// navScript derives the per-section PDF book navs (#383). Its output is a
// contract twice over: the slugs name release assets (so a rename breaks every
// published download URL), and the page sets are what each book contains (so a
// drift here is a book that silently loses chapters).
const navScript = "docs_pdf_nav.py"

// runNav runs the generator and returns stdout, stderr and the exit code.
func runNav(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatalf("python3 is required to test scripts/%s: %v", navScript, err)
	}

	cmd := exec.Command("python3", append([]string{navScript}, args...)...) //nolint:gosec // G204: test harness runs a repo script
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "nav script did not exit cleanly: %s%s", stdout.String(), stderr.String())
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// repoNav points the generator at this repo's mkdocs.yml. `go test` runs with
// the package directory as cwd, so the repo root is one level up.
func repoNav(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	config := filepath.Join("..", "mkdocs.yml")
	_, err := os.Stat(config)
	require.NoError(t, err, "repo mkdocs.yml must be reachable from the package dir")
	return runNav(t, append(args, "--mkdocs", config)...)
}

// TestDocsPdfNavSlugContract pins the book slugs, their order and the section
// titles they come from. The slugs are release asset names: renaming one
// breaks docs-site/downloads.md and every link already handed out.
func TestDocsPdfNavSlugContract(t *testing.T) {
	out, stderr, code := repoNav(t, "list", "--json")
	require.Equal(t, 0, code, stderr)

	var books []struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
		Pages int    `json:"pages"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &books), "list --json must be a JSON array: %s", out)

	want := []struct{ slug, title string }{
		{"getting-started", "Getting Started"},
		{"features", "Features"},
		{"planned-features", "Features (planned)"},
		{"architecture", "Architecture"},
		{"testing", "Testing"},
		{"plugin-reference", "Plugin Reference"},
		{"api", "API Specification"},
		{"operations", "Operations"},
		{"security", "Security & Compliance"},
		{"ui-integration", "UI Integration"},
		{"historical", "Historical"},
	}
	require.Len(t, books, len(want), "one book per mkdocs.yml top-level section; got %v", out)
	for i, w := range want {
		require.Equal(t, w.slug, books[i].Slug, "book %d slug", i)
		require.Equal(t, w.title, books[i].Title, "book %d title", i)
		require.NotZero(t, books[i].Pages, "book %s has no pages", books[i].Slug)
	}
}

// TestDocsPdfNavCoversRecentlyAddedPages is the regression for the hardcoded
// navs of 2026-07-30: pages added to mkdocs.yml after that date reached the
// site but no book, because mkdocs only warns about a missing nav entry.
// Every pair here must appear in the derived nav of its section.
func TestDocsPdfNavCoversRecentlyAddedPages(t *testing.T) {
	cases := []struct{ slug, path string }{
		{"getting-started", "whats-new.md"},
		{"features", "features/hosted-control-plane.md"},
		{"planned-features", "planned-features/hosted-control-plane-fleet-optimization.md"},
		{"planned-features", "planned-features/native-recommendation-processing.md"},
		{"planned-features", "planned-features/robne-saas-clowder-cutover.md"},
		{"architecture", "architecture/plugin-traits.md"},
		{"architecture", "architecture/librobne.md"},
		{"architecture", "architecture/ecosystem-comparison.md"},
		{"plugin-reference", "plugin-reference/slo.md"},
		{"plugin-reference", "plugin-reference/hcp.md"},
		{"api", "pagination.md"},
		{"historical", "historical/feature-status-archive.md"},
	}

	for _, c := range cases {
		t.Run(c.slug+"/"+c.path, func(t *testing.T) {
			out, stderr, code := repoNav(t, "pages", "--section", c.slug)
			require.Equal(t, 0, code, stderr)
			pages := strings.Split(strings.TrimSpace(out), "\n")
			require.Contains(t, pages, c.path, "%s must be in the %s book", c.path, c.slug)
		})
	}
}

// TestDocsPdfNavFlattensNestedSections pins the two shape rules a book nav must
// obey: leaves are flat (mkdocs-to-pdf iterates only top-level nav items, so a
// nested group renders as an empty book), and each page appears once. A leaf
// given without a label in mkdocs.yml stays unlabelled, so the page's own H1
// names the chapter instead of a guessed filename title.
func TestDocsPdfNavFlattensNestedSections(t *testing.T) {
	out, stderr, code := repoNav(t, "pages", "--section", "planned-features")
	require.Equal(t, 0, code, stderr)
	pages := strings.Split(strings.TrimSpace(out), "\n")

	count := 0
	for _, p := range pages {
		if p == "planned-features/local-mode.md" {
			count++
		}
	}
	require.Equal(t, 1, count, "local-mode.md must appear exactly once: %v", pages)

	nav, stderr, code := repoNav(t, "nav", "--section", "planned-features")
	require.Equal(t, 0, code, stderr)
	for _, line := range strings.Split(strings.TrimRight(nav, "\n"), "\n") {
		require.True(t, strings.HasPrefix(line, "  - "), "nav line must be a flat 2-space leaf: %q", line)
		require.False(t, strings.HasSuffix(strings.TrimSpace(line), ":"),
			"nav must carry no group lines (groups render as empty books): %q", line)
	}

	featuresNav, stderr, code := repoNav(t, "nav", "--section", "features")
	require.Equal(t, 0, code, stderr)
	require.Contains(t, strings.Split(featuresNav, "\n"), "  - features/index.md",
		"an unlabelled nav entry must stay unlabelled so the page H1 names the chapter")
}

// TestDocsPdfNavVerifyPassesOnRepoNav is the green control: the committed
// mkdocs.yml must satisfy the gate the workflow runs. If this fails, the gate
// is right and mkdocs.yml holds the bug (a moved page, an unmapped section).
func TestDocsPdfNavVerifyPassesOnRepoNav(t *testing.T) {
	out, stderr, code := runNav(t, "verify", "--mkdocs", filepath.Join("..", "mkdocs.yml"), "--site-root", "..")
	require.Equal(t, 0, code, "verify must pass on the repo nav; output: %s%s", out, stderr)
}

func mkdocsFixture(t *testing.T, body string) (mkdocsPath, siteRoot string) {
	t.Helper()
	siteRoot = t.TempDir()
	mkdocsPath = filepath.Join(siteRoot, "mkdocs.yml")
	require.NoError(t, os.WriteFile(mkdocsPath, []byte(body), 0o600))
	return mkdocsPath, siteRoot
}

// TestDocsPdfNavVerifyFailsOnMissingPage is the drift case that matters most: a
// page moves, mkdocs warns and drops the nav entry, and the book loses the
// chapter with no failure anywhere. The gate must name the path.
func TestDocsPdfNavVerifyFailsOnMissingPage(t *testing.T) {
	mkdocsPath, siteRoot := mkdocsFixture(t, `site_name: fixture
docs_dir: docs
nav:
  - Home: index.md
  - Features:
      - Present: features/present.md
      - Moved: features/missing.md
`)
	require.NoError(t, os.MkdirAll(filepath.Join(siteRoot, "docs", "features"), 0o750)) //nolint:gosec // G301: fixture tree
	require.NoError(t, os.WriteFile(filepath.Join(siteRoot, "docs", "index.md"), []byte("# Home\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(siteRoot, "docs", "features", "present.md"), []byte("# Present\n"), 0o600))

	out, stderr, code := runNav(t, "verify", "--mkdocs", mkdocsPath, "--site-root", siteRoot)
	require.Equal(t, 1, code, "verify must fail on a moved page; output: %s%s", out, stderr)
	require.Contains(t, stderr, "missing page: features/missing.md")
	require.NotContains(t, stderr, "features/present.md", "a present page must not be reported")
}

// TestDocsPdfNavVerifyFailsOnUnmappedSection covers the other direction: a new
// top-level section has no slug, so its book would publish under a guessed
// filename while the Downloads page points at a name that never appears.
func TestDocsPdfNavVerifyFailsOnUnmappedSection(t *testing.T) {
	mkdocsPath, siteRoot := mkdocsFixture(t, `site_name: fixture
docs_dir: docs
nav:
  - Home: index.md
  - Brand New Area:
      - One: new/one.md
      - Two: new/two.md
`)
	require.NoError(t, os.MkdirAll(filepath.Join(siteRoot, "docs", "new"), 0o750)) //nolint:gosec // G301: fixture tree
	require.NoError(t, os.WriteFile(filepath.Join(siteRoot, "docs", "index.md"), []byte("# Home\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(siteRoot, "docs", "new", "one.md"), []byte("# One\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(siteRoot, "docs", "new", "two.md"), []byte("# Two\n"), 0o600))

	out, stderr, code := runNav(t, "verify", "--mkdocs", mkdocsPath, "--site-root", siteRoot)
	require.Equal(t, 1, code, "verify must fail on an unmapped section; output: %s%s", out, stderr)
	require.Contains(t, stderr, "unmapped nav section: Brand New Area")
}

// TestDocsPdfNavNestedGroupKeepsItsChildren is Review Focus 4's real guard.
// The repo nav has no nested group today, so asserting "no group lines" on it
// proves nothing: a future author who groups five pages under `- GPU:` would
// get a book missing those chapters, a green `verify` (the leaves simply are
// not there to be missing) and a green suite. The fixture is the mutant.
func TestDocsPdfNavNestedGroupKeepsItsChildren(t *testing.T) {
	mkdocsPath, siteRoot := mkdocsFixture(t, `site_name: fixture
docs_dir: docs
nav:
  - Home: index.md
  - Features:
      - features/index.md
      - GPU:
          - MIG: features/gpu-mig.md
          - Classification: features/gpu-classification.md
      - Plain: features/plain.md
`)
	require.NoError(t, os.MkdirAll(filepath.Join(siteRoot, "docs", "features"), 0o750)) //nolint:gosec // G301: fixture tree
	for _, name := range []string{"gpu-mig.md", "gpu-classification.md", "plain.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(siteRoot, "docs", "features", name), []byte("# "+name+"\n"), 0o600))
	}

	pages, stderr, code := runNav(t, "pages", "--section", "features", "--mkdocs", mkdocsPath)
	require.Equal(t, 0, code, stderr)
	got := strings.Split(strings.TrimRight(pages, "\n"), "\n")
	require.Contains(t, got, "features/gpu-mig.md", "a grouped page must stay in the book")
	require.Contains(t, got, "features/gpu-classification.md", "a grouped page must stay in the book")
	require.Contains(t, got, "features/plain.md")

	nav, stderr, code := runNav(t, "nav", "--section", "features", "--mkdocs", mkdocsPath)
	require.Equal(t, 0, code, stderr)
	for _, line := range strings.Split(strings.TrimRight(nav, "\n"), "\n") {
		require.False(t, strings.HasSuffix(strings.TrimSpace(line), ":"),
			"a group must be flattened away, not emitted as an empty nav entry: %q", line)
	}
}

// TestDocsPdfDownloadsPageListsEveryBook ties the published page to the slug
// list: a new section (and so a new release asset) with no Downloads row is
// invisible to the people the books are for.
func TestDocsPdfDownloadsPageListsEveryBook(t *testing.T) {
	out, stderr, code := repoNav(t, "list", "--slugs")
	require.Equal(t, 0, code, stderr)

	body, err := os.ReadFile(filepath.Join("..", "docs-site", "downloads.md"))
	require.NoError(t, err)

	for _, slug := range strings.Split(strings.TrimSpace(out), "\n") {
		require.Contains(t, string(body), "/releases/download/docs-pdf/"+slug+".pdf",
			"docs-site/downloads.md must link the %s book", slug)
	}
}

// TestDocsPdfNavFragmentAndUnsupportedValue pins the two nav-value edges that
// used to vanish silently: a page named with a fragment belongs in the book,
// and a value that is neither a page nor an http(s) link is a nav bug.
func TestDocsPdfNavFragmentAndUnsupportedValue(t *testing.T) {
	mkdocsPath, siteRoot := mkdocsFixture(t, `site_name: fixture
docs_dir: docs
nav:
  - Home: index.md
  - Features:
      - Anchored: features/anchored.md#usage
      - Elsewhere: features/anchored.md
      - Broken: features/oops.txt
      - External: https://example.com/x
`)
	require.NoError(t, os.MkdirAll(filepath.Join(siteRoot, "docs", "features"), 0o750)) //nolint:gosec // G301: fixture tree
	require.NoError(t, os.WriteFile(filepath.Join(siteRoot, "docs", "features", "anchored.md"), []byte("# Anchored\n"), 0o600))

	pages, stderr, code := runNav(t, "pages", "--section", "features", "--mkdocs", mkdocsPath)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"features/anchored.md", "features/anchored.md"},
		strings.Split(strings.TrimRight(pages, "\n"), "\n"),
		"a fragment must name the page, not drop it")

	out, stderr, code := runNav(t, "verify", "--mkdocs", mkdocsPath, "--site-root", siteRoot)
	require.Equal(t, 1, code, "verify must fail on a non-page nav value; output: %s%s", out, stderr)
	require.Contains(t, stderr, "unsupported nav value: features/oops.txt")
	require.NotContains(t, stderr, "https://example.com/x", "an external link is legitimate")
	require.NotContains(t, stderr, "features/anchored.md", "the anchored page exists")
}
