package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #904: the CLI's stdout is a payload stream — `--format json > recs.json`
// must produce a file that parses as ONE JSON document. Logs (logrus and the
// config package's warnings) belong on stderr; before the fix they shared
// stdout, so every documented piping example was broken and the release
// smoke's grep was green on an unparsable file.
func TestRecommend_StdoutIsPureJSON(t *testing.T) {
	initCLI() // the same wiring main() runs — logs to stderr, payload to stdout

	// Capture the process stdout around a full recommend run.
	original := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	f := commonFlags{
		inputs:       []string{"testdata/hcp-capacity"},
		noUserConfig: true,
		now:          "2026-10-26T22:30:00Z",
		format:       "json",
		plugins:      "hcp",
	}
	result, execErr := executeRecommend(f)
	require.NoError(t, execErr)
	// The RunE chain: executeRecommend computes, writeRecs prints to
	// os.Stdout — both under the captured stream, as in the real command.
	writeErr := writeRecs(os.Stdout, result, f.format)

	// Restore stdout before reading, so test failures print normally.
	os.Stdout = original
	require.NoError(t, w.Close())
	out, readErr := io.ReadAll(r)
	require.NoError(t, readErr)
	require.NoError(t, writeErr)

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		head := string(out)
		if len(head) > 400 {
			head = head[:400]
		}
		t.Fatalf("stdout is not one JSON document (#904): %v\nstdout was:\n%s", err, head)
	}
	assert.NotNil(t, doc["hcp_recommendations"], "the payload must still be on stdout")
}
