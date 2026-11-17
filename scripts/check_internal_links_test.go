package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// #805: the internal link gate must slug the RENDERED heading text, not the
// raw Markdown line. A heading containing a link publishes GitHub's
// label-only anchor; the gate's slugger must strip the link destination and
// keep the label. Each test builds a fixture tree containing a COPY of the
// script (the bash wrapper cds to the script's own repo root, so the copy
// makes ROOT_DIR the fixture tree) and runs it there.

func runLinkGate(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(".."))
	require.NoError(t, err)
	script := filepath.Join(root, "scripts", "check-internal-links.sh")

	tree := t.TempDir()
	treeScript := filepath.Join(tree, "scripts", "check-internal-links.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(treeScript), 0o750)) //nolint:gosec // G301: fixture tree
	scriptContent, err := os.ReadFile(script)                        //nolint:gosec // G304: the repo's own gate script
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(treeScript, scriptContent, 0o750)) //nolint:gosec // G301,G302: the copy must stay executable

	for name, content := range files {
		path := filepath.Join(tree, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))     //nolint:gosec // G301: fixture tree
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600)) //nolint:gosec // G306: test fixture
	}

	cmd := exec.Command(treeScript) //nolint:gosec // G204: test harness runs the repo's own link gate on a fixture copy
	cmd.Dir = tree
	// The gate's soft mode (DOCS_LINKS_SOFT=1) exits 0 despite failures —
	// clear any inherited value so every test is meaningful on its own.
	cmd.Env = append(os.Environ(), "DOCS_LINKS_SOFT=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestCheckInternalLinks_HeadingWithLinkUsesLabelOnlySlug is #805's RED: a
// heading embeds an inline link; another page links to GitHub's published
// (label-only) anchor. The gate must accept the link — pre-fix it computes
// the URL-contaminated slug and rejects the CORRECT target.
func TestCheckInternalLinks_HeadingWithLinkUsesLabelOnlySlug(t *testing.T) {
	heading := "## BH-IDX-NODE ([#514](https://github.com/pgarciaq/ros-ocp-backend/issues/514)). Cluster-wide node BH queries"
	// GitHub's slug of the rendered text ("BH-IDX-NODE (#514). Cluster-wide
	// node BH queries"): the label "#514" stays, the destination goes.
	anchor := "#bh-idx-node-514-cluster-wide-node-bh-queries"

	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": heading + "\n\nbody\n",
		"docs/page-b.md": "see [the node section](page-a.md" + anchor + ")\n",
	})
	require.NoError(t, err, "a link to GitHub's published label-only anchor must be accepted; output: %s", out)
	require.Contains(t, out, "OK: every relative link resolves and every anchor exists")
}

// TestCheckInternalLinks_RejectsURLContaminatedAnchor pins the OTHER
// direction of #805: the URL-contaminated slug the gate used to compute
// must be REJECTED — a fail-open slugger (emitting both forms) would accept
// an anchor that 404s on GitHub, which is the exact defect this issue
// describes.
func TestCheckInternalLinks_RejectsURLContaminatedAnchor(t *testing.T) {
	heading := "## BH-IDX-NODE ([#514](https://github.com/pgarciaq/ros-ocp-backend/issues/514)). Cluster-wide node BH queries"
	contaminated := "#bh-idx-node-514httpsgithubcompgarciaqros-ocp-backendissues514-cluster-wide-node-bh-queries"

	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": heading + "\n\nbody\n",
		"docs/page-b.md": "see [the node section](page-a.md" + contaminated + ")\n",
	})
	require.Error(t, err, "the URL-contaminated anchor must be rejected — it 404s on GitHub")
	require.Contains(t, out, "no heading "+contaminated)
}

// TestCheckInternalLinks_DuplicateHeadingSuffixRule locks the
// live-oracle-verified duplicate rule: the first occurrence is bare, the
// rest get -1, -2, and so on.
func TestCheckInternalLinks_DuplicateHeadingSuffixRule(t *testing.T) {
	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": "## Classification Thresholds\n\none\n\n## Classification Thresholds\n\ntwo\n\n## Classification Thresholds\n\nthree\n",
		"docs/page-b.md": "see [first](page-a.md#classification-thresholds), [second](page-a.md#classification-thresholds-1), [third](page-a.md#classification-thresholds-2)\n",
	})
	require.NoError(t, err, "the -1/-2 duplicate suffix rule must hold; output: %s", out)
}

// TestCheckInternalLinks_LiteralHeadingEqualToGeneratedSuffix locks the
// cross-group collision case: a heading whose literal text equals another
// heading's generated -N slug must not be confused with it — GitHub's
// slugger dedupes against every anchor already emitted.
func TestCheckInternalLinks_LiteralHeadingEqualToGeneratedSuffix(t *testing.T) {
	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": "## Details\n\none\n\n## Details\n\ntwo\n\n## Details-1\n\nliteral\n",
		"docs/page-b.md": "see [literal](page-a.md#details-1-1) and [second duplicate](page-a.md#details-1)\n",
	})
	require.NoError(t, err, "a literal 'Details-1' heading must coexist with the generated 'Details-1'; output: %s", out)
}

// TestCheckInternalLinks_ReferenceStyleLinkInHeading checks that the label
// is kept and the reference name plus its definition contribute nothing to
// the slug.
func TestCheckInternalLinks_ReferenceStyleLinkInHeading(t *testing.T) {
	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": "## Sizing via [the engine][eng] rules\n\nbody\n\n[eng]: https://example.com/engine\n",
		"docs/page-b.md": "see [sizing](page-a.md#sizing-via-the-engine-rules)\n",
	})
	require.NoError(t, err, "a reference-style link in a heading must slug its label only; output: %s", out)
}

// TestCheckInternalLinks_ImageInHeadingSlugsAltText checks that, per
// #805's close criterion, an image in a heading keeps its alt text in the
// slug.
func TestCheckInternalLinks_ImageInHeadingSlugsAltText(t *testing.T) {
	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": "## Node ![node icon](img/node.png) utilization\n\nbody\n",
		"docs/page-b.md": "see [node utilization](page-a.md#node-node-icon-utilization)\n",
	})
	require.NoError(t, err, "an image in a heading must keep its alt text in the slug; output: %s", out)
}

// TestCheckInternalLinks_CodeSpanInHeadingIsLiteral pins the review's F2:
// a code span renders literally, so link syntax inside backticks must NOT
// be stripped — GitHub's anchor keeps the literal text.
func TestCheckInternalLinks_CodeSpanInHeadingIsLiteral(t *testing.T) {
	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": "## Run `[foo](bar)` now\n\nbody\n",
		"docs/page-b.md": "see [run](page-a.md#run-foobar-now)\n",
	})
	require.NoError(t, err, "link syntax inside a code span must slug as literal text (it renders literally); output: %s", out)
}

// TestCheckInternalLinks_BrokenAnchorStillFails checks the negative
// control: the gate keeps its teeth on a genuinely-broken anchor.
func TestCheckInternalLinks_BrokenAnchorStillFails(t *testing.T) {
	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": "## A Real Heading\n\nbody\n",
		"docs/page-b.md": "see [nowhere](page-a.md#this-heading-does-not-exist)\n",
	})
	require.Error(t, err, "a broken anchor must still fail the gate")
	require.Contains(t, out, "no heading #this-heading-does-not-exist in docs/page-a.md")
	require.False(t, strings.Contains(out, "soft mode"), "the gate must not run in soft mode from tests")
}

// TestCheckInternalLinks_UnicodeHeadingSlugsGitHubExact pins #869: the
// slugger uses github-slugger's exact removal class, not Python's \w
// keep-list. Superscripts (²) are stripped (the live case: the audit page's
// MERGE-ALLOC heading); accented letters and CJK are kept (the
// must-not-regress direction); emoji are stripped (the reference strips
// them — \w also did, so this pins the port against naively keeping them).
func TestCheckInternalLinks_UnicodeHeadingSlugsGitHubExact(t *testing.T) {
	out, err := runLinkGate(t, map[string]string{
		"docs/page-a.md": "## Merge/alloc is on² — extra copy\n\nbody\n\n## Café münchen 中\n\nbody\n\n## 🚀 Launch checklist\n\nbody\n",
		"docs/page-b.md": "see [merge](page-a.md#mergealloc-is-on--extra-copy), [café](page-a.md#café-münchen-中), [launch](page-a.md#-launch-checklist)\n",
	})
	require.NoError(t, err, "the unicode anchors must match github-slugger exactly; output: %s", out)
}
