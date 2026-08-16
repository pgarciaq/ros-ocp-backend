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

func oneDayNodeCSV(ns, wl, cluster, nodeName string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,workload,workload_type,container_name,pod,node,cluster_id,node_capacity_cpu_cores,node_capacity_memory_bytes,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n")
	for h := 0; h < 24; h++ {
		start := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h)
		end := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h+1)
		if h == 23 {
			end = "2026-08-02 00:00:00 +0000 UTC"
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,deployment,%s,%s-0,%s,%s,4,8589934592,0.2,0.05,104857600,52428800\n",
			start, end, ns, wl, wl, wl, nodeName, cluster)
	}
	return b.String()
}

func oneDayGPUCSV(ns, wl, cluster, nodeName string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,workload,workload_type,container_name,pod,node,cluster_id,accelerator_model_name,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n")
	for h := 0; h < 24; h++ {
		start := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h)
		end := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h+1)
		if h == 23 {
			end = "2026-08-02 00:00:00 +0000 UTC"
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,deployment,%s,%s-0,%s,%s,NVIDIA A100-SXM4-80GB,0.2,0.05,104857600,52428800\n",
			start, end, ns, wl, wl, wl, nodeName, cluster)
	}
	return b.String()
}

func namespaceOneDayCSV(ns string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg\n")
	for h := 0; h < 24; h++ {
		start := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h)
		end := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h+1)
		if h == 23 {
			end = "2026-08-02 00:00:00 +0000 UTC"
		}
		fmt.Fprintf(&b, "%s,%s,%s,0.500,0.250,1073741824,536870912\n", start, end, ns)
	}
	return b.String()
}

func TestRecommend_NamespacePluginStdout(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceOneDayCSV("kube-system")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "namespace",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.NamespaceRecs)
	var shortCost *int
	for i, r := range result.NamespaceRecs {
		assert.Equal(t, "kube-system", r.Namespace)
		if r.Term == "short" && r.Engine == "cost" {
			shortCost = &i
		}
	}
	require.NotNil(t, shortCost, "expected a short/cost namespace recommendation")
	assert.Greater(t, result.NamespaceRecs[*shortCost].RecCPURequestMC, int64(0))

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 2, env.Version)
	require.NotNil(t, env.NamespaceRecommendations)
	assert.NotEmpty(t, *env.NamespaceRecommendations)
}

func TestRecommend_DefaultPluginsIgnoresNamespaceFiles(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceOneDayCSV("kube-system")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        cwd,
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Recs)
	assert.Empty(t, result.NamespaceRecs)
}

func TestRecommend_NodePluginStdout(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayNodeCSV("app", "api", "cluster-a", "worker-1")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "node",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.NodeRecs)
	assert.Equal(t, "worker-1", result.NodeRecs[0].Node)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 3, env.Version)
	require.NotNil(t, env.NodeRecommendations)
	assert.NotEmpty(t, *env.NodeRecommendations)
	assert.Nil(t, env.GPURecommendations)
}

func TestRecommend_GPUPluginStdout(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayGPUCSV("app", "api", "cluster-a", "gpu-1")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "gpu",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.GPURecs)
	assert.Equal(t, "app", result.GPURecs[0].Namespace)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", result.GPURecs[0].Rec.GPUModelName)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 4, env.Version)
	require.NotNil(t, env.GPURecommendations)
	assert.NotEmpty(t, *env.GPURecommendations)
	require.NotNil(t, env.GPUTimeslicingRecommendations)
}

func TestRecommend_DefaultPluginsIgnoresNodeGPU(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayNodeCSV("app", "api", "cluster-a", "worker-1")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        cwd,
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Recs)
	assert.Empty(t, result.NodeRecs)
	assert.Empty(t, result.GPURecs)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	assert.NotContains(t, buf.String(), "node_recommendations")
	assert.NotContains(t, buf.String(), "gpu_recommendations")
}

func TestRecommend_NodeWithoutContainerCSVError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceOneDayCSV("kube-system")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "node",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container ROS")
}

func TestRecommend_NamespaceOnlyDefaultPluginError(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceOneDayCSV("kube-system")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := computeRecommendations(commonFlags{
		input:        csvPath,
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

func TestRecommend_PathANamespaceOnlyError(t *testing.T) {
	err := rejectFileOnlyPostgresInput([]string{"namespace"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "postgres")
}

func TestRecommend_PathANodeOnlyError(t *testing.T) {
	err := rejectFileOnlyPostgresInput([]string{"node"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "postgres")
	err = rejectFileOnlyPostgresInput([]string{"gpu"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "postgres")
	require.NoError(t, rejectFileOnlyPostgresInput([]string{"container", "node"}))
}

func TestWarnFileOnlyNotPersisted(t *testing.T) {
	assert.Equal(t, []string{"namespace"}, fileOnlyPluginNames([]string{"namespace"}))
	assert.Equal(t, []string{"node", "gpu"}, fileOnlyPluginNames([]string{"container", "node", "gpu"}))
	assert.Empty(t, fileOnlyPluginNames([]string{"container"}))
}
