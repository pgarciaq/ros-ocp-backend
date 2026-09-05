package csv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// withMalformedHook captures ReportMalformedJSON sites for the test and
// restores the default no-op reporter afterwards. No t.Parallel: the hook is
// process-wide mutable state.
func withMalformedHook(t *testing.T) *[]string {
	t.Helper()
	var got []string
	types.SetMalformedJSONReporter(func(site string) { got = append(got, site) })
	t.Cleanup(func() { types.SetMalformedJSONReporter(nil) })
	return &got
}

func TestParseSnapshotRows_MalformedLabelsKeptEmpty(t *testing.T) {
	got := withMalformedHook(t)
	rows, skipped, err := ParseSnapshotRows(strings.NewReader(
		"namespace,snapshot_name,creation_timestamp,labels\n" +
			"production,snap-a,2025-12-01T03:00:00Z,\"{bad json\"\n"))
	require.NoError(t, err)
	assert.Equal(t, 0, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, map[string]string{}, rows[0].Labels)
	assert.Equal(t, []string{types.SiteSnapshotLabels}, *got)
}

func TestParseSnapshotRows_TypeMismatchLabelsForcedEmpty(t *testing.T) {
	got := withMalformedHook(t)
	// {"a":"1","b":2} is syntactically valid JSON but b is not a string:
	// encoding/json leaves a partial map ({a:1, b:""}). Keep-{} (#538)
	// requires deterministic empty, never partial.
	rows, _, err := ParseSnapshotRows(strings.NewReader(
		"namespace,snapshot_name,creation_timestamp,labels\n" +
			"production,snap-b,2025-12-01T03:00:00Z,\"{ \"\"a\"\": \"\"1\"\", \"\"b\"\": 2 }\"\n"))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, map[string]string{}, rows[0].Labels)
	assert.NotContains(t, rows[0].Labels, "a")
	assert.Equal(t, []string{types.SiteSnapshotLabels}, *got)
}

func TestParseSnapshotRows_ValidLabelsUnchanged(t *testing.T) {
	got := withMalformedHook(t)
	rows, _, err := ParseSnapshotRows(strings.NewReader(
		"namespace,snapshot_name,creation_timestamp,labels\n" +
			"production,snap-c,2025-12-01T03:00:00Z,\"{\"\"app\"\":\"\"x\"\"}\"\n" +
			"production,snap-d,2025-12-01T03:00:00Z,{}\n" +
			"production,snap-e,2025-12-01T03:00:00Z,\n"))
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, map[string]string{"app": "x"}, rows[0].Labels)
	assert.Equal(t, map[string]string{}, rows[1].Labels)
	assert.Equal(t, map[string]string{}, rows[2].Labels)
	assert.Empty(t, *got, "valid/empty labels must not report")
}
