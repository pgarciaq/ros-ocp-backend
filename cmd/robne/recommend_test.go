package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	recs, err := computeRecommendations(commonFlags{
		input:        csvPath,
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	var shortCost *int
	for i, r := range recs {
		assert.Equal(t, "app", r.Namespace)
		assert.Equal(t, "api", r.Workload)
		if r.Term == "short" && r.Engine == "cost" {
			shortCost = &i
		}
	}
	require.NotNil(t, shortCost, "expected a short/cost recommendation from one day of data")
	assert.Greater(t, recs[*shortCost].RecCPURequestMC, int64(0))
	assert.Greater(t, recs[*shortCost].RecMemRequestKiB, int64(0))
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

func TestWriteRecs_UnknownFormat(t *testing.T) {
	err := writeRecs(os.Stdout, nil, "xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --format")
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
