package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ce *cliExit
	if errors.As(err, &ce) {
		return ce.Code
	}
	return 1
}

func writeJSONFile(t *testing.T, dir, name string, env recommendJSON) string {
	t.Helper()
	path := filepath.Join(dir, name)
	raw, err := json.MarshalIndent(env, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(raw, '\n'), 0o600))
	return path
}

func baseEnvelope() recommendJSON {
	return recommendJSON{
		Version:     1,
		ClusterID:   "cluster-a",
		Now:         "2026-08-01T02:00:00Z",
		SkippedRows: 0,
		Recommendations: []containerOut{
			{
				Namespace:            "app",
				Workload:             "api",
				WorkloadType:         "deployment",
				ContainerName:        "api",
				Term:                 "short",
				Engine:               "cost",
				RecCPURequestMC:      58,
				RecCPULimitMC:        61,
				RecMemRequestKiB:     58880,
				RecMemLimitKiB:       61824,
				CurrentCPURequestMC:  200,
				CurrentMemRequestKiB: 102400,
				IdleState:            "active",
				Category:             "oversized",
			},
		},
	}
}

func TestCompareEnvelopes_IdenticalIncludingReorderedRows(t *testing.T) {
	left := baseEnvelope()
	right := baseEnvelope()
	right.Recommendations = []containerOut{left.Recommendations[0], {
		Namespace:     "app",
		Workload:      "web",
		WorkloadType:  "deployment",
		ContainerName: "web",
		Term:          "short",
		Engine:        "cost",
		IdleState:     "active",
	}}
	left.Recommendations = []containerOut{right.Recommendations[1], right.Recommendations[0]}
	report, differs, err := compareEnvelopes(left, right)
	require.NoError(t, err)
	assert.False(t, differs)
	assert.Empty(t, report)
}

func TestCompareEnvelopes_FieldChangeAndMetadata(t *testing.T) {
	left := baseEnvelope()
	right := baseEnvelope()
	right.Recommendations[0].RecCPURequestMC = 70
	right.ClusterID = "cluster-b"
	right.SkippedRows = 2
	report, differs, err := compareEnvelopes(left, right)
	require.NoError(t, err)
	require.True(t, differs)
	assert.Contains(t, report, "cluster_id: cluster-a → cluster-b")
	assert.Contains(t, report, "skipped_rows: 0 → 2")
	assert.Contains(t, report, "recommendations: 0 added, 0 removed, 1 changed")
	assert.Contains(t, report, "rec_cpu_request_mc: 58 → 70")
}

func TestCompareEnvelopes_AddedRemoved(t *testing.T) {
	left := baseEnvelope()
	right := baseEnvelope()
	right.Recommendations = []containerOut{{
		Namespace:     "other",
		Workload:      "job",
		WorkloadType:  "job",
		ContainerName: "job",
		Term:          "short",
		Engine:        "cost",
	}}
	report, differs, err := compareEnvelopes(left, right)
	require.NoError(t, err)
	require.True(t, differs)
	assert.Contains(t, report, "1 added, 1 removed, 0 changed")
	assert.Contains(t, report, "+ other/job/job/job short/cost")
	assert.Contains(t, report, "- app/api/deployment/api short/cost")
}

func TestCompareEnvelopes_EmptySiblingVsMissing(t *testing.T) {
	left := baseEnvelope()
	right := baseEnvelope()
	empty := []namespaceOut{}
	right.NamespaceRecommendations = &empty
	report, differs, err := compareEnvelopes(left, right)
	require.NoError(t, err)
	require.True(t, differs)
	assert.Contains(t, report, "namespace_recommendations: missing → []")
}

func TestCompareEnvelopes_VersionMismatch(t *testing.T) {
	left := baseEnvelope()
	right := baseEnvelope()
	right.Version = 2
	_, _, err := compareEnvelopes(left, right)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version mismatch")
}

func TestCompareEnvelopes_DuplicateKeys(t *testing.T) {
	left := baseEnvelope()
	left.Recommendations = append(left.Recommendations, left.Recommendations[0])
	right := baseEnvelope()
	_, _, err := compareEnvelopes(left, right)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate row key")
}

func TestRunDiff_ExitCodes(t *testing.T) {
	dir := t.TempDir()
	left := writeJSONFile(t, dir, "left.json", baseEnvelope())
	same := writeJSONFile(t, dir, "same.json", baseEnvelope())
	changed := baseEnvelope()
	changed.Recommendations[0].RecCPURequestMC = 99
	right := writeJSONFile(t, dir, "right.json", changed)
	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("not-json"), 0o600))

	var buf bytes.Buffer
	err := runDiff(&buf, left, same)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))
	assert.Equal(t, "identical\n", buf.String())

	buf.Reset()
	err = runDiff(&buf, left, right)
	assert.Equal(t, 1, exitCode(err))
	assert.Contains(t, buf.String(), "rec_cpu_request_mc")

	buf.Reset()
	err = runDiff(&buf, left, bad)
	assert.Equal(t, 2, exitCode(err))
}

func TestDiffCmd_Cobra(t *testing.T) {
	dir := t.TempDir()
	left := writeJSONFile(t, dir, "left.json", baseEnvelope())
	right := writeJSONFile(t, dir, "right.json", baseEnvelope())
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"diff", left, right})
	require.NoError(t, root.Execute())
	assert.Equal(t, "identical\n", out.String())
}

func oneDayShortCostEnvelope(t *testing.T) recommendJSON {
	t.Helper()
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)
	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "container",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	var one []containerOut
	for _, row := range env.Recommendations {
		if row.Term == "short" && row.Engine == "cost" {
			one = append(one, row)
		}
	}
	require.Len(t, one, 1)
	env.Recommendations = one
	return env
}

func TestDiff_CheckedInGoldenEnvelope(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	goldenPath := filepath.Join(wd, "testdata", "golden_envelope_v1.json")
	got := oneDayShortCostEnvelope(t)
	if os.Getenv("WRITE_GOLDEN") == "1" {
		raw, err := json.MarshalIndent(got, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(goldenPath, append(raw, '\n'), 0o600))
	}
	raw, err := os.ReadFile(goldenPath) //nolint:gosec // G304: checked-in testdata
	require.NoError(t, err, "checked-in v1 envelope golden; WRITE_GOLDEN=1 to refresh")
	var want recommendJSON
	require.NoError(t, json.Unmarshal(raw, &want))
	report, differs, err := compareEnvelopes(want, got)
	require.NoError(t, err)
	assert.False(t, differs, report)

	dir := t.TempDir()
	left := writeJSONFile(t, dir, "left.json", want)
	right := writeJSONFile(t, dir, "right.json", got)
	var buf bytes.Buffer
	require.NoError(t, runDiff(&buf, left, right))
	assert.Equal(t, "identical\n", buf.String())
}
