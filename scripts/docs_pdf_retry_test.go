package scripts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retryScript is the #960 retry wrapper docs-pdf.sh uses around mmdc. Chrome's
// screenshot call deadlocks intermittently on shared runners, so the wrapper
// retries that signature only. The boundary is the whole point: retrying a
// Mermaid parse error would hide a real content break behind a second attempt.
const retryScript = "docs-pdf/retry_command.sh"

// timeoutSignature is what mmdc prints when the screenshot call deadlocks. It
// is the only failure the wrapper retries.
const timeoutSignature = "ProtocolError: Page.captureScreenshot timed out. Increase the 'protocolTimeout' setting"

// retryStub writes a fake command that emits the given line to stderr and exits
// with the given status, on the attempts listed in failOn. Attempts are counted
// through countFile so the test can assert how many times it ran.
func retryStub(t *testing.T, dir, countFile, line string, status int, failOn ...string) string {
	t.Helper()
	path := filepath.Join(dir, "stub.sh")
	body := fmt.Sprintf(`#!/usr/bin/env bash
count_file=%q
fail_on=(%s)
n=$(( $(cat "$count_file" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$count_file"
for f in "${fail_on[@]}"; do
  if [[ "$f" == "$n" ]]; then
    echo %q >&2
    exit %d
  fi
done
echo "rendered perfectly"
exit 0
`, countFile, strings.Join(failOn, " "), line, status)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o700)) //nolint:gosec // G306: the test must execute the stub
	return path
}

func runRetry(t *testing.T, args ...string) (string, int) {
	t.Helper()
	full := append([]string{retryScript}, args...)
	cmd := exec.Command("bash", full...) //nolint:gosec // G204: test-built command, not user input
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if assert.ErrorAs(t, err, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	return stderr.String(), code
}

func retryArgs(logDir, attempts string, command ...string) []string {
	args := []string{
		"--attempts", attempts,
		"--log-dir", logDir,
		"--label", "page",
		"--retry-pattern", "ProtocolError: .*timed out",
		"--pause", "0",
		"--",
	}
	return append(args, command...)
}

func TestRetryCommand(t *testing.T) {
	t.Run("succeeds on the first attempt without retrying", func(t *testing.T) {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "logs")
		count := filepath.Join(dir, "count")
		stub := retryStub(t, dir, count, timeoutSignature, 1)

		stderr, code := runRetry(t, retryArgs(logDir, "3", "bash", stub)...)

		require.Equal(t, 0, code)
		assert.NotContains(t, stderr, "retrying")
		entries, err := os.ReadDir(logDir)
		require.NoError(t, err)
		require.Len(t, entries, 1, "one attempt, one log")
		body, err := os.ReadFile(filepath.Join(logDir, "page-attempt1.log")) //nolint:gosec // G304: test temp dir
		require.NoError(t, err)
		assert.Contains(t, string(body), "rendered perfectly")
	})

	t.Run("retries the timeout signature and recovers", func(t *testing.T) {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "logs")
		count := filepath.Join(dir, "count")
		stub := retryStub(t, dir, count, timeoutSignature, 1, "1")

		stderr, code := runRetry(t, retryArgs(logDir, "3", "bash", stub)...)

		require.Equal(t, 0, code, "a hang on attempt 1 must not fail the build")
		assert.Contains(t, stderr, "recovered on attempt 2/3")
		entries, err := os.ReadDir(logDir)
		require.NoError(t, err)
		require.Len(t, entries, 2)
		first, err := os.ReadFile(filepath.Join(logDir, "page-attempt1.log")) //nolint:gosec // G304: test temp dir
		require.NoError(t, err)
		assert.Contains(t, string(first), "ProtocolError", "the failed attempt's log is what the workflow uploads")
	})

	t.Run("does not retry a parse error", func(t *testing.T) {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "logs")
		count := filepath.Join(dir, "count")
		stub := retryStub(t, dir, count, "Error: Parse error on line 3", 7, "1")

		stderr, code := runRetry(t, retryArgs(logDir, "3", "bash", stub)...)

		require.Equal(t, 7, code, "the command's own exit status survives")
		assert.Contains(t, stderr, "non-retryable")
		assert.NotContains(t, stderr, "retrying")
		attempts, err := os.ReadFile(count) //nolint:gosec // G304: test temp dir
		require.NoError(t, err)
		assert.Equal(t, "1", strings.TrimSpace(string(attempts)), "a content break must fail on the first attempt")
	})

	t.Run("gives up after the attempt budget", func(t *testing.T) {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "logs")
		count := filepath.Join(dir, "count")
		stub := retryStub(t, dir, count, timeoutSignature, 1, "1", "2")

		stderr, code := runRetry(t, retryArgs(logDir, "2", "bash", stub)...)

		require.NotEqual(t, 0, code)
		assert.Contains(t, stderr, "still failing after 2 attempts")
		entries, err := os.ReadDir(logDir)
		require.NoError(t, err)
		require.Len(t, entries, 2, "the budget bounds the attempts")
	})

	t.Run("rejects a non-positive attempt budget", func(t *testing.T) {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "logs")

		stderr, code := runRetry(t, retryArgs(logDir, "0", "true")...)

		require.Equal(t, 2, code)
		assert.Contains(t, stderr, "positive integer")
	})
}
