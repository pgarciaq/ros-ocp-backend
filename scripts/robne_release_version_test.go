package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// #877: the robne release version is a pinned contract —
// 0.<phase>.YYYYMMDD-HHMM-<sha7> with a UTC commit-time stamp and the FIRST 7
// of the SHA (matching git %h and ${GITHUB_SHA::7}, so a release joins 1:1 with
// the quay image tag built from the same commit). make robne-release and the
// nightly task run this script; release-robne.yml re-asserts stamp+sha against
// the commit. These tests pin the format and the loud guards so a silent
// format change cannot ship releases (e.g. a local-timezone stamp, an un-padded
// HHMM, or a last-7 SHA would all fail here).

const (
	fixtureCommitterDate = "2026-11-18T02:15:00+00:00"
	// The stamp the fixture commit must produce: 2026-11-18 02:15 UTC, HHMM
	// zero-padded — "215" or "2:15" must never pass.
	fixtureStamp = "20261118-0215"
)

func initVersionRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixture repository
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com",
			"GIT_AUTHOR_DATE="+fixtureCommitterDate,
			"GIT_COMMITTER_DATE="+fixtureCommitterDate,
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("init", "-q")
	git("checkout", "-q", "-b", "pgarciaq-rosocp-superpowers-phase17")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "fixture.txt"), []byte("fixture\n"), 0o600))
	git("add", ".")
	git("commit", "-q", "-m", "fixture commit")
	return repo
}

func runVersionScript(t *testing.T, script string, repo string, extraEnv ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(script) //nolint:gosec // G204: test harness runs the repo's own release script on a fixture repo
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), extraEnv...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

func repoHeadSHA7(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD") //nolint:gosec // G204: fixture repository
	cmd.Dir = repo
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))[:7]
}

func TestRobneReleaseVersion_FormatPinnedToContract(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(".."))
	require.NoError(t, err)
	script := filepath.Join(root, "scripts", "robne-release-version.sh")

	repo := initVersionRepo(t)
	stdout, stderr, err := runVersionScript(t, script, repo)
	require.NoError(t, err, "stderr: %s", stderr)

	version := strings.TrimSpace(stdout)
	require.Equal(t, "0.17."+fixtureStamp+"-"+repoHeadSHA7(t, repo), version,
		"exact contract: 0.<phase>.<UTC commit stamp>-<first-7 sha>")
	require.Regexp(t, regexp.MustCompile(`^0\.17\.\d{8}-\d{4}-[0-9a-f]{7}$`), version,
		"shape guard: 8-digit date, zero-padded HHMM, 7-char sha")
}

func TestRobneReleaseVersion_StampIsUTCRegardlessOfLocalTZ(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(".."))
	require.NoError(t, err)
	script := filepath.Join(root, "scripts", "robne-release-version.sh")

	repo := initVersionRepo(t)
	// If the script forgot TZ=UTC, America/New_York renders the 02:15Z commit
	// as 20261117-2115 (previous calendar day) — the assertion catches it.
	stdout, stderr, err := runVersionScript(t, script, repo, "TZ=America/New_York")
	require.NoError(t, err, "stderr: %s", stderr)
	require.Contains(t, strings.TrimSpace(stdout), fixtureStamp,
		"stamp must be the commit's UTC time even under a non-UTC local timezone")
}

func TestRobneReleaseVersion_RejectsNonPhaseBranch(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(".."))
	require.NoError(t, err)
	script := filepath.Join(root, "scripts", "robne-release-version.sh")

	repo := initVersionRepo(t)
	cmd := exec.Command("git", "checkout", "-q", "-b", "main") //nolint:gosec // G204: fixture repository
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git checkout main: %s", out)

	_, stderr, err := runVersionScript(t, script, repo)
	require.Error(t, err, "a branch without a phase number must fail loudly")
	require.Contains(t, stderr, "pgarciaq-rosocp-superpowers-phase",
		"the failure must name the accepted branch shape")
}

func TestRobneReleaseVersion_RejectsDirtyTree(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(".."))
	require.NoError(t, err)
	script := filepath.Join(root, "scripts", "robne-release-version.sh")

	repo := initVersionRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "fixture.txt"), []byte("uncommitted\n"), 0o600))

	_, stderr, err := runVersionScript(t, script, repo)
	require.Error(t, err, "a dirty tree must fail loudly: the tag would not describe the worktree")
	require.Contains(t, stderr, "dirty", "the failure must name the dirty tree")
}
