package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecommend_ShortTermExists(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Recs)
	var shortCost *int
	for i, r := range result.Recs {
		assert.Equal(t, "app", r.Namespace)
		assert.Equal(t, "api", r.Workload)
		if r.Term == "short" && r.Engine == "cost" {
			shortCost = &i
		}
	}
	require.NotNil(t, shortCost, "expected a short/cost recommendation from one day of data")
	assert.Greater(t, result.Recs[*shortCost].RecCPURequestMC, int64(0))
	assert.Greater(t, result.Recs[*shortCost].RecMemRequestKiB, int64(0))
}

func TestRecommend_GoldenShortTerm(t *testing.T) {
	const pinnedNow = "2026-08-01T02:00:00Z"
	wantRaw, err := os.ReadFile("testdata/golden_short_cost.json")
	require.NoError(t, err)
	var want struct {
		Term             string `json:"term"`
		Engine           string `json:"engine"`
		RecCPURequestMC  int64  `json:"rec_cpu_request_mc"`
		RecMemRequestKiB int64  `json:"rec_mem_request_kib"`
	}
	require.NoError(t, json.Unmarshal(wantRaw, &want))

	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		noUserConfig: true,
		now:          pinnedNow,
		format:       "json",
	})
	require.NoError(t, err)
	assert.Equal(t, "cluster-a", result.ClusterID)
	assert.Equal(t, 0, result.SkippedRows)
	assert.Equal(t, pinnedNow, result.Now.UTC().Format(time.RFC3339))

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, recommendJSONVersion, env.Version)
	assert.Equal(t, pinnedNow, env.Now)

	var got *containerOut
	for i := range env.Recommendations {
		row := &env.Recommendations[i]
		if row.Term == want.Term && row.Engine == want.Engine {
			got = row
			break
		}
	}
	require.NotNil(t, got, "expected %s/%s row in JSON envelope", want.Term, want.Engine)
	assert.Equal(t, want.RecCPURequestMC, got.RecCPURequestMC)
	assert.Equal(t, want.RecMemRequestKiB, got.RecMemRequestKiB)
	assert.Nil(t, got.EstimatedSavingsCents)
}

func TestRecommend_ClusterUUIDMismatch(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("cluster_uuid: cluster-b\n"), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := computeRecommendations(commonFlags{
		input:        csvPath,
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disagrees")
}

func oneDayCSV(ns, wl, cluster string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,workload,workload_type,container_name,pod,cluster_id,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n")
	for h := 0; h < 24; h++ {
		start := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h)
		end := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h+1)
		if h == 23 {
			end = "2026-08-02 00:00:00 +0000 UTC"
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,deployment,%s,%s-0,%s,0.2,0.05,104857600,52428800\n",
			start, end, ns, wl, wl, wl, cluster)
	}
	return b.String()
}
