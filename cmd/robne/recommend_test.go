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

	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
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
		plugins:      "container",
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

func TestRecommend_DefaultPluginsRunsNamespaceFiles(t *testing.T) {
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
	require.NotEmpty(t, result.NamespaceRecs)
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

func TestRecommend_DefaultPluginsRunsNodeSkipsGPUWithoutModel(t *testing.T) {
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
	require.NotEmpty(t, result.NodeRecs)
	assert.Empty(t, result.GPURecs)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	assert.Contains(t, buf.String(), "node_recommendations")
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

func TestRecommend_DefaultPluginsRunsNamespaceOnly(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceOneDayCSV("kube-system")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.NamespaceRecs)
}

func TestRecommend_PathAApplySchemaRejected(t *testing.T) {
	_, err := executeRecommend(commonFlags{
		input:        "postgres://localhost/robne",
		plugins:      "namespace",
		applySchema:  true,
		noUserConfig: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recompute")
}

func storageTwoDayCSV(ns, pvcName string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,pod,persistentvolumeclaim,persistentvolume,storageclass,persistentvolumeclaim_capacity_bytes,persistentvolumeclaim_usage_byte_seconds\n")
	for day := 1; day <= 2; day++ {
		for h := 0; h < 24; h++ {
			start := fmt.Sprintf("2026-05-%02d %02d:00:00+00:00", day, h)
			endH := h + 1
			endDay := day
			if h == 23 {
				endH = 0
				endDay = day + 1
			}
			end := fmt.Sprintf("2026-05-%02d %02d:00:00+00:00", endDay, endH)
			fmt.Fprintf(&b, "%s,%s,%s,app-pod-1,%s,pv-data,gp3,10737418240,360000000000\n",
				start, end, ns, pvcName)
		}
	}
	return b.String()
}

func TestRecommend_PVCPluginStdout(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_storage_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(storageTwoDayCSV("production", "data-pvc")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "pvc",
		noUserConfig: true,
		now:          "2026-05-03T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.PVCRecs)
	assert.Equal(t, "production", result.PVCRecs[0].Namespace)
	assert.Equal(t, "data-pvc", result.PVCRecs[0].PVC)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 5, env.Version)
	require.NotNil(t, env.PVCRecommendations)
	assert.NotEmpty(t, *env.PVCRecommendations)
}

func TestRecommend_PVCWithoutStorageCSVError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "pvc",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage")
}

func TestValidate_PVCStorageOnly(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_storage_usage.csv"), []byte(storageTwoDayCSV("production", "data-pvc")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	require.NoError(t, runValidate(commonFlags{
		input:        cwd,
		plugins:      "pvc",
		noUserConfig: true,
	}))
}

func TestRecommend_DefaultPluginsRunsPVCFiles(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_storage_usage.csv"), []byte(storageTwoDayCSV("production", "data-pvc")), 0o600))

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
	require.NotEmpty(t, result.PVCRecs)
}

func vmUsageHeader() string {
	return "interval_start,interval_end,vm_name,namespace,node_name,guest_os,cpu_usage_mc,cpu_request_mc,cpu_limit_mc,memory_usage_kib,memory_request_kib,memory_available_kib,disk_allocated_bytes,filesystem_used_bytes,filesystem_capacity_bytes,disk_read_iops,disk_write_iops,disk_read_bytes_per_sec,disk_write_bytes_per_sec"
}

func vmTwoDayCSV(ns, name string) string {
	var b strings.Builder
	b.WriteString(vmUsageHeader())
	b.WriteByte('\n')
	for day := 1; day <= 2; day++ {
		for h := 0; h < 24; h++ {
			start := fmt.Sprintf("2026-05-%02dT%02d:00:00Z", day, h)
			endH := h + 1
			endDay := day
			if h == 23 {
				endH = 0
				endDay = day + 1
			}
			end := fmt.Sprintf("2026-05-%02dT%02d:00:00Z", endDay, endH)
			fmt.Fprintf(&b, "%s,%s,%s,%s,worker-1,linux,1500,2000,4000,1048576,2097152,,10737418240,,,,,,\n",
				start, end, name, ns)
		}
	}
	return b.String()
}

func TestRecommend_VMPluginStdout(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_vm_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(vmTwoDayCSV("production", "web-vm")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "vm",
		noUserConfig: true,
		now:          "2026-05-03T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.VMRecs)
	assert.Equal(t, "production", result.VMRecs[0].Namespace)
	assert.Equal(t, "web-vm", result.VMRecs[0].VMName)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 6, env.Version)
	require.NotNil(t, env.VMRecommendations)
	assert.NotEmpty(t, *env.VMRecommendations)
}

func TestRecommend_VMWithoutUsageCSVError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "vm",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "VM usage")
}

func TestValidate_VMUsageOnly(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_vm_usage.csv"), []byte(vmTwoDayCSV("production", "web-vm")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	require.NoError(t, runValidate(commonFlags{
		input:        cwd,
		plugins:      "vm",
		noUserConfig: true,
	}))
}

func namespaceQuotaOneDayCSV(ns, quotaName string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,quota_name,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg,cpu_request_namespace_used\n")
	for h := 0; h < 24; h++ {
		start := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h)
		end := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h+1)
		if h == 23 {
			end = "2026-08-02 00:00:00 +0000 UTC"
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,2.000,0.250,1073741824,536870912,0.500\n", start, end, ns, quotaName)
	}
	return b.String()
}

func namespaceQuotaTwoDayCSV(ns, quotaName string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,quota_name,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg,cpu_request_namespace_used\n")
	for day := 1; day <= 2; day++ {
		start := fmt.Sprintf("2026-08-%02d 00:00:00 +0000 UTC", day)
		end := fmt.Sprintf("2026-08-%02d 01:00:00 +0000 UTC", day)
		fmt.Fprintf(&b, "%s,%s,%s,%s,%.3f,0.250,1073741824,536870912,0.500\n", start, end, ns, quotaName, float64(day))
	}
	return b.String()
}

func threeDayCSV(ns, wl, cluster string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,workload,workload_type,container_name,pod,cluster_id,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n")
	for day := 1; day <= 3; day++ {
		for h := 0; h < 24; h++ {
			start := fmt.Sprintf("2026-08-%02d %02d:00:00 +0000 UTC", day, h)
			endH := h + 1
			endDay := day
			if h == 23 {
				endH = 0
				endDay = day + 1
			}
			end := fmt.Sprintf("2026-08-%02d %02d:00:00 +0000 UTC", endDay, endH)
			fmt.Fprintf(&b, "%s,%s,%s,%s,deployment,%s,%s-0,%s,0.2,0.05,104857600,52428800\n",
				start, end, ns, wl, wl, wl, cluster)
		}
	}
	return b.String()
}

func TestRecommend_QuotaPluginStdout(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceQuotaOneDayCSV("app", "compute-resources")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.QuotaRecs)
	assert.Equal(t, "app", result.QuotaRecs[0].Namespace)
	assert.Equal(t, "compute-resources", result.QuotaRecs[0].QuotaName)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 7, env.Version)
	require.NotNil(t, env.QuotaRecommendations)
	assert.NotEmpty(t, *env.QuotaRecommendations)
	assert.Nil(t, (*env.QuotaRecommendations)[0].EstimatedSavingsCents)
}

func TestRecommend_QuotaNoQuotaNameEmptyRecs(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceOneDayCSV("kube-system")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.QuotaRecs)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":7`)
	assert.Contains(t, compact, `"quota_recommendations":[]`)
	assert.NotContains(t, compact, `"quota_recommendations":null`)
}

func TestRecommend_QuotaWithoutNamespaceCSVError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

func TestValidate_QuotaNamespaceOnly(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayCSV("app", "compute-resources")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	require.NoError(t, runValidate(commonFlags{
		input:        cwd,
		plugins:      "quota",
		noUserConfig: true,
	}))
}

func TestRecommend_DefaultPluginsRunsQuotaFiles(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayCSV("app", "compute-resources")), 0o600))

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
	require.NotEmpty(t, result.QuotaRecs)
}

func TestRecommend_QuotaUsesContainerAggregates(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(threeDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayCSV("app", "compute-resources")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "quota",
		noUserConfig: true,
		now:          "2026-08-04T02:00:00Z",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs, "quota-only must not emit container recs")
	require.NotEmpty(t, result.QuotaRecs)
	assert.Greater(t, result.QuotaRecs[0].Recommended.CPURequestMillicores, int64(0),
		"medium/cost container recs in the same namespace must feed quota aggregates")
}

func TestRecommend_DefaultPluginsRunsVMFiles(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_vm_usage.csv"), []byte(vmTwoDayCSV("production", "web-vm")), 0o600))

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
	require.NotEmpty(t, result.VMRecs)
}

func clusterQuotaOneDayCSV(name, namespaces, cpuHard, cpuUsed string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,cluster_quota_name,cpu_request_hard,cpu_request_used,memory_request_hard,namespaces\n")
	ns := namespaces
	if strings.Contains(ns, ",") {
		ns = `"` + ns + `"`
	}
	for h := 0; h < 24; h++ {
		start := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h)
		end := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h+1)
		if h == 23 {
			end = "2026-08-02 00:00:00 +0000 UTC"
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s,1073741824,%s\n", start, end, name, cpuHard, cpuUsed, ns)
	}
	return b.String()
}

func threeDayMultiNSCSV(cluster string, nss ...string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,workload,workload_type,container_name,pod,cluster_id,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n")
	for _, ns := range nss {
		wl := ns + "-api"
		for day := 1; day <= 3; day++ {
			for h := 0; h < 24; h++ {
				start := fmt.Sprintf("2026-08-%02d %02d:00:00 +0000 UTC", day, h)
				endH := h + 1
				endDay := day
				if h == 23 {
					endH = 0
					endDay = day + 1
				}
				end := fmt.Sprintf("2026-08-%02d %02d:00:00 +0000 UTC", endDay, endH)
				fmt.Fprintf(&b, "%s,%s,%s,%s,deployment,%s,%s-0,%s,0.2,0.05,104857600,52428800\n",
					start, end, ns, wl, wl, wl, cluster)
			}
		}
	}
	return b.String()
}

func namespaceQuotaOneDayMultiCSV(pairs [][2]string) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,quota_name,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg,cpu_request_namespace_used\n")
	for _, pair := range pairs {
		ns, quotaName := pair[0], pair[1]
		for h := 0; h < 24; h++ {
			start := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h)
			end := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h+1)
			if h == 23 {
				end = "2026-08-02 00:00:00 +0000 UTC"
			}
			fmt.Fprintf(&b, "%s,%s,%s,%s,2.000,0.250,1073741824,536870912,0.500\n", start, end, ns, quotaName)
		}
	}
	return b.String()
}

func sumQuotaCPURecommended(recs []quota.QuotaRec, namespaces ...string) int64 {
	filter := map[string]struct{}{}
	for _, ns := range namespaces {
		filter[ns] = struct{}{}
	}
	var sum int64
	for _, r := range recs {
		if len(filter) > 0 {
			if _, ok := filter[r.Namespace]; !ok {
				continue
			}
		}
		sum += r.Recommended.CPURequestMillicores
	}
	return sum
}

func TestRecommend_ClusterQuotaPluginStdout(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_cluster_quota.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(clusterQuotaOneDayCSV("team-a", "", "10.000", "3.000")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "cluster_quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	assert.Empty(t, result.QuotaRecs, "cluster_quota-only must not emit quota siblings")
	require.NotEmpty(t, result.ClusterQuotaRecs)
	assert.Equal(t, "team-a", result.ClusterQuotaRecs[0].ClusterQuotaName)
	assert.Equal(t, int64(1073741824), result.ClusterQuotaRecs[0].Snapshot.MemoryRequestHardBytes, "CRQ memory stays bytes")

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 8, env.Version)
	require.NotNil(t, env.ClusterQuotaRecommendations)
	assert.NotEmpty(t, *env.ClusterQuotaRecommendations)
	assert.Nil(t, (*env.ClusterQuotaRecommendations)[0].EstimatedSavingsCents)
	assert.Nil(t, env.QuotaRecommendations)
}

func TestRecommend_ClusterQuotaNoHardLimitsEmptyRecs(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_cluster_quota.csv")
	var b strings.Builder
	b.WriteString("interval_start,interval_end,cluster_quota_name,cpu_request_hard,cpu_request_used\n")
	for h := 0; h < 24; h++ {
		start := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h)
		end := fmt.Sprintf("2026-08-01 %02d:00:00 +0000 UTC", h+1)
		if h == 23 {
			end = "2026-08-02 00:00:00 +0000 UTC"
		}
		fmt.Fprintf(&b, "%s,%s,team-a,0,0\n", start, end)
	}
	require.NoError(t, os.WriteFile(csvPath, []byte(b.String()), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "cluster_quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.ClusterQuotaRecs)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":8`)
	assert.Contains(t, compact, `"cluster_quota_recommendations":[]`)
	assert.NotContains(t, compact, `"cluster_quota_recommendations":null`)
}

func TestRecommend_ClusterQuotaWithoutCSVError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "cluster_quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster-quota")
}

func TestValidate_ClusterQuotaOnly(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_cluster_quota.csv"), []byte(clusterQuotaOneDayCSV("team-a", "", "10.000", "3.000")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	require.NoError(t, runValidate(commonFlags{
		input:        cwd,
		plugins:      "cluster_quota",
		noUserConfig: true,
	}))
}

func TestRecommend_DefaultPluginsRunsClusterQuotaFiles(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_cluster_quota.csv"), []byte(clusterQuotaOneDayCSV("team-a", "", "10.000", "3.000")), 0o600))

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
	require.NotEmpty(t, result.ClusterQuotaRecs)
}

func TestRecommend_ClusterQuotaWithoutNamespaceStillEmits(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_cluster_quota.csv"), []byte(clusterQuotaOneDayCSV("team-a", "app", "10.000", "3.000")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "cluster_quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.ClusterQuotaRecs)
	assert.Equal(t, int64(0), result.ClusterQuotaRecs[0].Expl.NSQuotaCPUSumMC)
	assert.Equal(t, int64(3300), result.ClusterQuotaRecs[0].Recommended.CPURequestMillicores,
		"used 3000 mc × 110% headroom with zero namespace aggregates")
}

func TestRecommend_ClusterQuotaEmptyNamespacesSumsAll(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(threeDayMultiNSCSV("cluster-a", "app", "prod")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayMultiCSV([][2]string{{"app", "compute-a"}, {"prod", "compute-b"}})), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_cluster_quota.csv"), []byte(clusterQuotaOneDayCSV("team-a", "", "10.000", "0.100")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "quota,cluster_quota",
		noUserConfig: true,
		now:          "2026-08-04T02:00:00Z",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.Len(t, result.QuotaRecs, 2)
	require.NotEmpty(t, result.ClusterQuotaRecs)
	want := sumQuotaCPURecommended(result.QuotaRecs)
	assert.Equal(t, want, result.ClusterQuotaRecs[0].Expl.NSQuotaCPUSumMC)
	assert.Greater(t, want, int64(0))
}

func TestRecommend_ClusterQuotaNamespacesFilterMembership(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(threeDayMultiNSCSV("cluster-a", "app", "prod")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayMultiCSV([][2]string{{"app", "compute-a"}, {"prod", "compute-b"}})), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_cluster_quota.csv"), []byte(clusterQuotaOneDayCSV("team-a", "app", "10.000", "0.100")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "quota,cluster_quota",
		noUserConfig: true,
		now:          "2026-08-04T02:00:00Z",
	})
	require.NoError(t, err)
	require.Len(t, result.QuotaRecs, 2)
	require.NotEmpty(t, result.ClusterQuotaRecs)
	appOnly := sumQuotaCPURecommended(result.QuotaRecs, "app")
	all := sumQuotaCPURecommended(result.QuotaRecs)
	assert.Equal(t, appOnly, result.ClusterQuotaRecs[0].Expl.NSQuotaCPUSumMC)
	assert.Greater(t, all, appOnly)
}

func TestRecommend_ClusterQuotaTwoQuotasInOneNamespaceBothCount(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(threeDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayMultiCSV([][2]string{{"app", "compute-a"}, {"app", "compute-b"}})), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_cluster_quota.csv"), []byte(clusterQuotaOneDayCSV("team-a", "app", "10.000", "0.100")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "quota,cluster_quota",
		noUserConfig: true,
		now:          "2026-08-04T02:00:00Z",
	})
	require.NoError(t, err)
	require.Len(t, result.QuotaRecs, 2)
	require.NotEmpty(t, result.ClusterQuotaRecs)
	assert.Equal(t, sumQuotaCPURecommended(result.QuotaRecs), result.ClusterQuotaRecs[0].Expl.NSQuotaCPUSumMC)
	assert.Equal(t, result.QuotaRecs[0].Recommended.CPURequestMillicores*2, result.ClusterQuotaRecs[0].Expl.NSQuotaCPUSumMC)
}

func snapshotInventoryCSV() string {
	return strings.Join([]string{
		"namespace,snapshot_name,creation_timestamp,source_pvc_name,restore_size_bytes,source_pvc_exists,restored_pvc_count",
		"app,snap-a,2026-07-01T00:00:00Z,data-pvc,1073741824,true,0",
	}, "\n")
}

func snapshotHourlyCSV() string {
	return strings.Join([]string{
		"namespace,snapshot_name,creation_timestamp,source_pvc_name,restore_size_bytes,source_pvc_exists,interval_start,interval_end",
		"app,snap-a,2026-07-01T00:00:00Z,data-pvc,100,true,2026-08-01T00:00:00Z,2026-08-01T01:00:00Z",
		"app,snap-a,2026-07-01T00:00:00Z,data-pvc,200,false,2026-08-01T01:00:00Z,2026-08-01T02:00:00Z",
	}, "\n")
}

func TestRecommend_SnapshotPluginStdout(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_snapshot_inventory.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(snapshotInventoryCSV()), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "snapshot",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.Len(t, result.SnapshotRecs, 1)
	assert.Equal(t, "app", result.SnapshotRecs[0].Namespace)
	assert.Equal(t, "snap-a", result.SnapshotRecs[0].SnapshotName)
	require.NotNil(t, result.SnapshotRecs[0].NotificationCodes)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, recommendJSONVersionWithSnapshot, env.Version)
	require.NotNil(t, env.SnapshotRecommendations)
	require.Len(t, *env.SnapshotRecommendations, 1)
	require.NotNil(t, (*env.SnapshotRecommendations)[0].NotificationCodes)
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.NotContains(t, compact, `"notification_codes":null`)
}

func TestRecommend_SnapshotWithoutCSVError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := computeRecommendations(commonFlags{
		input:        cwd,
		plugins:      "snapshot",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot")
}

func TestRecommend_DefaultPluginsSkipsMissingSnapshot(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

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
	assert.Empty(t, result.SnapshotRecs)
	assert.False(t, pluginEnabled(result.plugins, "snapshot"))

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	assert.NotContains(t, buf.String(), "snapshot_recommendations")
}

func TestRecommend_DefaultPluginsRunsSnapshotFiles(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_snapshot_inventory.csv"), []byte(snapshotInventoryCSV()), 0o600))

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
	require.NotEmpty(t, result.SnapshotRecs)
}

func TestRecommend_SnapshotHourlyCollapseOneRec(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_snapshot_inventory.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(snapshotHourlyCSV()), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "snapshot",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.NoError(t, err)
	require.Len(t, result.SnapshotRecs, 1)
	assert.Equal(t, int64(200), result.SnapshotRecs[0].RestoreSizeBytes)
	assert.False(t, result.SnapshotRecs[0].SourcePVCExists)
}

func TestRecommend_SnapshotEmptyInventoryEmptySibling(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_snapshot_inventory.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("namespace,snapshot_name,creation_timestamp\n"), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "snapshot",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.SnapshotRecs)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":9`)
	assert.Contains(t, compact, `"snapshot_recommendations":[]`)
	assert.NotContains(t, compact, `"snapshot_recommendations":null`)
}

func TestValidate_SnapshotOnly(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_snapshot_inventory.csv"), []byte(snapshotInventoryCSV()), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	require.NoError(t, runValidate(commonFlags{
		input:        cwd,
		plugins:      "snapshot",
		noUserConfig: true,
	}))
}

func TestRecommend_DefaultPluginsCSVNeedsSingleEntity(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Recs)
	err = writeRecs(bytes.NewBuffer(nil), result, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
}

func weekdayCSV(ns, wl, cluster string, day time.Time) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,workload,workload_type,container_name,pod,cluster_id,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n")
	day = day.UTC()
	next := day.Add(24 * time.Hour).Format("2006-01-02")
	date := day.Format("2006-01-02")
	for h := 0; h < 24; h++ {
		start := fmt.Sprintf("%s %02d:00:00 +0000 UTC", date, h)
		end := fmt.Sprintf("%s %02d:00:00 +0000 UTC", date, h+1)
		if h == 23 {
			end = fmt.Sprintf("%s 00:00:00 +0000 UTC", next)
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,deployment,%s,%s-0,%s,0.2,0.05,104857600,52428800\n",
			start, end, ns, wl, wl, wl, cluster)
	}
	return b.String()
}

func writeBusinessHoursYAML(t *testing.T, dir string) {
	t.Helper()
	body := `business_hours:
  enabled: true
  timezone: UTC
  days: [monday, tuesday, wednesday, thursday, friday]
  start_time: "09:00"
  end_time: "17:00"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "robne.yaml"), []byte(body), 0o600))
}

func TestRecommend_BusinessHoursWeekendEmptySiblings(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	writeBusinessHoursYAML(t, cwd)

	t.Setenv("HOME", t.TempDir())
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
	require.NotEmpty(t, result.Recs, "all_hours recs still emit on a Saturday")
	assert.True(t, result.businessHours)
	assert.Empty(t, result.BHRecs, "weekend samples drop from the business_hours stream")
	assert.Empty(t, result.BHDigests)

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, recommendJSONVersionWithBusinessHours, env.Version)
	require.NotNil(t, env.BusinessHoursRecommendations)
	assert.Empty(t, *env.BusinessHoursRecommendations)
	require.NotNil(t, env.BusinessHoursNamespaceRecommendations)
	assert.Empty(t, *env.BusinessHoursNamespaceRecommendations)
}

func TestRecommend_BusinessHoursWeekdayFiltersSamples(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	monday := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(csvPath, []byte(weekdayCSV("app", "api", "cluster-a", monday)), 0o600))
	writeBusinessHoursYAML(t, cwd)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "container",
		noUserConfig: true,
		now:          "2026-08-04T00:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Recs)
	require.NotEmpty(t, result.BHRecs)
	require.Len(t, result.Digests, 1)
	require.Len(t, result.BHDigests, 1)
	assert.Equal(t, int64(24), result.Digests[0].Row.SampleCount)
	assert.Equal(t, int64(8), result.BHDigests[0].Row.SampleCount, "09:00–17:00 keeps hours 9–16")
}

func TestRecommend_BusinessHoursCSVError(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	writeBusinessHoursYAML(t, cwd)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input:        csvPath,
		plugins:      "container",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
	})
	require.NoError(t, err)
	err = writeRecs(bytes.NewBuffer(nil), result, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
	assert.Contains(t, err.Error(), "schedule")
	err = writeRecs(bytes.NewBuffer(nil), result, "table")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
}

func TestRequirePathABusinessHoursDigests(t *testing.T) {
	err := requirePathABusinessHoursDigests(fileLoad{
		bhEnabled: true,
		plugins:   []string{"container"},
		orgID:     "1234567",
		clusterID: "cluster-a",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "business_hours digest")

	err = requirePathABusinessHoursDigests(fileLoad{
		bhEnabled: true,
		plugins:   []string{"namespace"},
		orgID:     "1234567",
		clusterID: "cluster-a",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace digest")

	require.NoError(t, requirePathABusinessHoursDigests(fileLoad{
		bhEnabled: true,
		plugins:   []string{"node"},
	}))
	require.NoError(t, requirePathABusinessHoursDigests(fileLoad{
		bhEnabled: false,
		plugins:   []string{"container"},
	}))
}
