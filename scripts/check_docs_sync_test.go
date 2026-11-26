package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// check-docs-sync.sh's one-sided drift signal (#816): for each registered
// pair, report when exactly one side changed on the branch vs the merge base
// with the default branch. These tests drive the script against a temp git
// repo covering the four Q1 cases plus the stub-pair direction.
const syncScript = "check-docs-sync.sh"

const syncTestPairs = `docs/alpha.md|docs-site/alpha.md|-|Class A pair
docs/beta.md|docs-site/beta.md|stub|stub pair (public-led)
`

// newSyncRepo builds a temp git repo: main holds the base content of the pair
// files and the pairs registry; a feature branch is checked out with the given
// edits applied and committed.
func newSyncRepo(t *testing.T, edits map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...) //nolint:gosec // G204: test harness runs git in a temp dir
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	git("init", "-b", "main")
	base := map[string]string{
		"docs/alpha.md":      "alpha base\n",
		"docs-site/alpha.md": "alpha base\n",
		"docs/beta.md":       "beta base\n",
		"docs-site/beta.md":  "beta base\n",
		"pairs.txt":          syncTestPairs,
	}
	for path, content := range base {
		full := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	git("add", "-A")
	git("commit", "-m", "base")
	git("checkout", "-b", "feature")

	for path, content := range edits {
		full := filepath.Join(dir, path)
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	if len(edits) > 0 {
		git("add", "-A")
		git("commit", "-m", "edits")
	}
	return dir
}

// runSync runs the checker against a custom root, base ref and pairs file,
// returning combined output and the exit code.
func runSync(t *testing.T, root string) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatalf("bash is required to test scripts/%s: %v", syncScript, err)
	}
	cmd := exec.Command("bash", syncScript, //nolint:gosec // G204: test harness runs a repo script
		"--root", root, "--base", "main", "--pairs", filepath.Join(root, "pairs.txt"))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "sync script did not exit cleanly: %s", out.String())
		code = exitErr.ExitCode()
	}
	return out.String(), code
}

func TestCheckDocsSync_OneSidedDrift(t *testing.T) {
	cases := []struct {
		name       string
		edits      map[string]string
		wantLines  []string // substrings that must appear on ONE-SIDED lines
		wantSilent bool
	}{
		{
			name:       "neither side touched is silent",
			edits:      map[string]string{},
			wantSilent: true,
		},
		{
			name:       "both sides touched is silent (attendance satisfied)",
			edits:      map[string]string{"docs/alpha.md": "a2\n", "docs-site/alpha.md": "a2\n"},
			wantSilent: true,
		},
		{
			name:      "internal-only edit reports naming both paths",
			edits:     map[string]string{"docs/alpha.md": "a2\n"},
			wantLines: []string{"docs/alpha.md", "docs-site/alpha.md", "internal"},
		},
		{
			name:      "public-only edit reports naming both paths",
			edits:     map[string]string{"docs-site/alpha.md": "a2\n"},
			wantLines: []string{"docs/alpha.md", "docs-site/alpha.md", "public"},
		},
		{
			name:       "stub pair public-only edit is silent (public-led by design)",
			edits:      map[string]string{"docs-site/beta.md": "b2\n"},
			wantSilent: true,
		},
		{
			name:      "stub pair internal-only edit reports (the stub was edited)",
			edits:     map[string]string{"docs/beta.md": "b2\n"},
			wantLines: []string{"docs/beta.md", "docs-site/beta.md"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newSyncRepo(t, tc.edits)
			out, code := runSync(t, root)

			require.Equal(t, 0, code, "advisory mode always exits 0; output:\n%s", out)
			oneSided := []string{}
			for _, line := range strings.Split(out, "\n") {
				// Table rows are printf '%-9s' padded ("ONE-SIDED  path");
				// the explanatory note ("ONE-SIDED is the signal: ...") must not match.
				if strings.HasPrefix(line, "ONE-SIDED  ") {
					oneSided = append(oneSided, line)
				}
			}
			if tc.wantSilent {
				require.Empty(t, oneSided, "expected no ONE-SIDED lines; got:\n%s", out)
			} else {
				require.Len(t, oneSided, 1, "expected exactly one ONE-SIDED line; got:\n%s", out)
				for _, want := range tc.wantLines {
					require.Contains(t, oneSided[0], want, "ONE-SIDED line must name %q; got:\n%s", want, out)
				}
			}
		})
	}
}

func TestCheckDocsSync_NoMergeBaseSkipsOneSided(t *testing.T) {
	// A base ref that does not exist: the script must not crash and must say
	// the one-sided detection was skipped.
	root := newSyncRepo(t, map[string]string{"docs/alpha.md": "a2\n"})
	cmd := exec.Command("bash", syncScript, //nolint:gosec // G204: test harness runs a repo script
		"--root", root, "--base", "no-such-ref", "--pairs", filepath.Join(root, "pairs.txt"))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "script must not fail on a missing base ref: %s", out.String())
		code = exitErr.ExitCode()
	}
	require.Equal(t, 0, code, "missing base ref is a skip-with-notice, not a failure; output:\n%s", out.String())
	require.Contains(t, out.String(), "skipped", "the notice must say one-sided detection was skipped")
	require.NotContains(t, out.String(), "ONE-SIDED  ", "no signal without a base")
}
