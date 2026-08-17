package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
	"github.com/redhatinsights/ros-ocp-backend/librobne/snapshot"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToContainerExplainOut_OmitsPascalCaseExpl(t *testing.T) {
	rec := sampleRec(nil)
	rec.Expl.DataDays = 1
	rec.Expl.OOMBumpApplied = true
	out := toContainerExplainOut(rec, scheduleAllHours)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "CPUCostPctMC")
	assert.NotContains(t, string(raw), "OOMBumpApplied")
	assert.Contains(t, string(raw), `"data_days"`)
	assert.Contains(t, string(raw), `"oom_bump_applied"`)
	assert.Equal(t, 1, out.DataDays)
	assert.True(t, out.OOMBumpApplied)
	assert.Equal(t, scheduleAllHours, out.Schedule)
}

func TestRunExplain_OneDayFixture(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{
			input:        csvPath,
			noUserConfig: true,
			now:          "2026-08-01T02:00:00Z",
		},
		namespace: "app",
		workload:  "api",
		container: "api",
		term:      "short",
		engine:    "cost",
	})
	require.NoError(t, err)
	var out containerExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "app", out.Namespace)
	assert.Equal(t, "api", out.Workload)
	assert.Equal(t, "deployment", out.WorkloadType)
	assert.Equal(t, "api", out.ContainerName)
	assert.Equal(t, "short", out.Term)
	assert.Equal(t, "cost", out.Engine)
	assert.Equal(t, scheduleAllHours, out.Schedule)
	assert.Equal(t, int64(58), out.RecCPURequestMC)
	assert.Equal(t, int64(58880), out.RecMemRequestKiB)
	assert.Equal(t, 1, out.DataDays)
	assert.False(t, out.OOMBumpApplied)
	assert.Greater(t, out.CPUCostPctMC, int64(0))
	assert.Greater(t, out.MemCostPctKiB, int64(0))
	assert.NotContains(t, buf.String(), "CPUCostPctMC")
}

func TestRunExplain_NoMatch(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "missing",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching container")
}

func TestRunExplain_AmbiguousWorkloadType(t *testing.T) {
	matches := []types.ContainerRec{
		{Namespace: "app", Workload: "api", WorkloadType: "deployment", ContainerName: "api", Term: "short", Engine: "cost"},
		{Namespace: "app", Workload: "api", WorkloadType: "daemonset", ContainerName: "api", Term: "short", Engine: "cost"},
	}
	_, err := selectContainerRec(matches, explainFlags{
		namespace: "app", workload: "api", container: "api", term: "short", engine: "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--workload-type")

	got, err := selectContainerRec(matches, explainFlags{
		namespace: "app", workload: "api", workloadType: "daemonset", container: "api", term: "short", engine: "cost",
	})
	require.NoError(t, err)
	assert.Equal(t, "daemonset", got.WorkloadType)
}

func TestRunExplain_RejectsRecommendEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONFile(t, dir, "env.json", baseEnvelope())
	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: path, noUserConfig: true},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not read recommend JSON envelopes")
}

func TestResolveExplainPlugin_OneTypeAtATime(t *testing.T) {
	got, err := resolveExplainPlugin("")
	require.NoError(t, err)
	assert.Equal(t, "container", got)

	got, err = resolveExplainPlugin("namespace")
	require.NoError(t, err)
	assert.Equal(t, "namespace", got)

	_, err = resolveExplainPlugin("namespace,node")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one entity type at a time")
}

func TestRunExplain_TwoPluginsError(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{
			input:        csvPath,
			plugins:      "namespace,node",
			noUserConfig: true,
			now:          "2026-08-01T02:00:00Z",
		},
		namespace: "app",
		term:      "short",
		engine:    "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one entity type at a time")
}

func TestRunExplain_YAMLPluginsDoesNotSelectType(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("plugins:\n  - namespace\n"), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
	})
	require.NoError(t, err)
	var out containerExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "api", out.ContainerName)
}

func TestRunExplain_NamespaceInapplicableFlag(t *testing.T) {
	err := validateExplainIdentity("namespace", "", explainFlags{
		namespace: "app", workload: "api", container: "api", term: "short", engine: "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--container")
}

func TestRunExplain_GPUBothIdentitiesError(t *testing.T) {
	_, err := gpuExplainKind("gpu", explainFlags{container: "api", node: "gpu-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not both")
}

func TestRunExplain_BusinessHoursScheduleRequiresYAML(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
		schedule:    scheduleBusinessHours,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "business_hours.enabled")
}

func TestRunExplain_BusinessHoursWeekendNoMatch(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	writeBusinessHoursYAML(t, cwd)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
		schedule:    scheduleBusinessHours,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching container")
}

func TestRunExplain_BusinessHoursWeekday(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	monday := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(csvPath, []byte(weekdayCSV("app", "api", "cluster-a", monday)), 0o600))
	writeBusinessHoursYAML(t, cwd)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-04T00:00:00Z"},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
		schedule:    scheduleBusinessHours,
	})
	require.NoError(t, err)
	var out containerExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, scheduleBusinessHours, out.Schedule)
	assert.Equal(t, 1, out.DataDays)
}

func TestRunExplain_BusinessHoursOtherPluginError(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayNodeCSV("app", "api", "cluster-a", "worker-1")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: csvPath, plugins: "node", noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		node:        "worker-1",
		term:        "short",
		engine:      "cost",
		schedule:    scheduleBusinessHours,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container and namespace only")
}

func TestRunExplain_Namespace(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceOneDayCSV("kube-system")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, plugins: "namespace", noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "kube-system",
		term:        "short",
		engine:      "cost",
	})
	require.NoError(t, err)
	var out namespaceExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "kube-system", out.Namespace)
	assert.Equal(t, "short", out.Term)
	assert.Equal(t, scheduleAllHours, out.Schedule)
	assert.Greater(t, out.RecCPURequestMC, int64(0))
	assert.Greater(t, out.CPUCostPctMC, int64(0))
	assert.NotContains(t, buf.String(), "CPUCostPctMC")
}

func TestRunExplain_Node(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayNodeCSV("app", "api", "cluster-a", "worker-1")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, plugins: "node", noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		node:        "worker-1",
		term:        "short",
		engine:      "cost",
	})
	require.NoError(t, err)
	var out nodeExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "worker-1", out.Node)
	assert.Equal(t, "short", out.Term)
	assert.NotContains(t, buf.String(), "TargetUtilizationBP")
}

func TestRunExplain_GPUMIG(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayGPUCSV("app", "api", "cluster-a", "gpu-1")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, plugins: "gpu", noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
	})
	require.NoError(t, err)
	var out gpuExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "app", out.Namespace)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", out.GPUModelName)
	assert.NotContains(t, buf.String(), "HasProfilingData")
}

func TestRunExplain_GPUTimeslicing(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayGPUCSV("app", "api", "cluster-a", "gpu-1")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := computeRecommendations(commonFlags{
		input: csvPath, plugins: "gpu", noUserConfig: true, now: "2026-08-01T02:00:00Z",
	})
	require.NoError(t, err)
	if len(result.GPUTimeslicing) == 0 {
		t.Skip("fixture did not produce timeslicing recs")
	}
	ts := result.GPUTimeslicing[0]

	var buf bytes.Buffer
	err = runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, plugins: "gpu", noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		node:        ts.NodeName,
		gpuModel:    ts.GPUModel,
		term:        ts.Term,
	})
	require.NoError(t, err)
	var out gpuTimeslicingExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, ts.NodeName, out.Node)
	assert.Equal(t, ts.GPUModel, out.GPUModel)
}

func TestRunExplain_PVC(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_storage_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(storageTwoDayCSV("production", "data-pvc")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, plugins: "pvc", noUserConfig: true, now: "2026-05-03T02:00:00Z"},
		namespace:   "production",
		pvc:         "data-pvc",
		term:        "short",
	})
	require.NoError(t, err)
	var out pvcExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "production", out.Namespace)
	assert.Equal(t, "data-pvc", out.PVC)
	assert.Contains(t, buf.String(), `"usage_ratio"`)
	assert.NotContains(t, buf.String(), "ClassificationReason")
}

func TestRunExplain_VM(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_vm_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(vmTwoDayCSV("production", "web-vm")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, plugins: "vm", noUserConfig: true, now: "2026-05-03T02:00:00Z"},
		namespace:   "production",
		vmName:      "web-vm",
		term:        "short_term",
		engine:      "cost",
	})
	require.NoError(t, err)
	var out vmExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "web-vm", out.VMName)
	assert.Equal(t, "short_term", out.Term)
	assert.NotContains(t, buf.String(), "SizingBranch")
}

func TestRunExplain_Quota(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceQuotaOneDayCSV("app", "compute")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, plugins: "quota", noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "app",
		quotaName:   "compute",
	})
	require.NoError(t, err)
	var out quotaExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "app", out.Namespace)
	assert.Equal(t, "compute", out.QuotaName)
	assert.NotContains(t, buf.String(), "HeadroomBP")
}

func TestRunExplain_ClusterQuota(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_cluster_quota.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(clusterQuotaOneDayCSV("team-a", "", "10.000", "3.000")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags:      commonFlags{input: csvPath, plugins: "cluster_quota", noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		clusterQuotaName: "team-a",
	})
	require.NoError(t, err)
	var out clusterQuotaExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "team-a", out.ClusterQuotaName)
	assert.NotContains(t, buf.String(), "NSQuotaCPUSumMC")
}

func TestRunExplain_Snapshot(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_snapshot_inventory.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(snapshotInventoryCSV()), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags:  commonFlags{input: csvPath, plugins: "snapshot", noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:    "app",
		snapshotName: "snap-a",
	})
	require.NoError(t, err)
	var out snapshotExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "snap-a", out.SnapshotName)
	assert.NotEmpty(t, out.ClassificationRule)
	assert.NotContains(t, buf.String(), "ClassificationRule")
}

func TestToNamespaceExplainOut_OmitsPascalCaseExpl(t *testing.T) {
	rec := sampleNSRec()
	rec.Expl.DataDays = 2
	out := toNamespaceExplainOut(rec, scheduleAllHours)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "CPUCostPctMC")
	assert.Contains(t, string(raw), `"data_days"`)
	assert.Equal(t, 2, out.DataDays)
}

func TestToPVCExplainOut_IncludesUsageRatio(t *testing.T) {
	rec := pvc.PVCRec{Namespace: "ns", PVC: "vol", Term: "short", UsageRatio: 0.4, GrowthBytesPerDay: 10, Expl: types.PVCExplanationFactors{DataDays: 3, ClassificationReason: "near_full"}}
	out := toPVCExplainOut(rec)
	assert.Equal(t, 0.4, out.UsageRatio)
	assert.Equal(t, int64(10), out.GrowthBytesPerDay)
	assert.Equal(t, "near_full", out.ClassificationReason)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "ClassificationReason")
}

func TestSelectQuotaRec_AmbiguousRecommendationType(t *testing.T) {
	recs := []quota.QuotaRec{
		{Namespace: "app", QuotaName: "compute", RecommendationType: "raise"},
		{Namespace: "app", QuotaName: "compute", RecommendationType: "tighten"},
	}
	_, err := selectQuotaRec(recs, explainFlags{namespace: "app", quotaName: "compute"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--recommendation-type")
	got, err := selectQuotaRec(recs, explainFlags{namespace: "app", quotaName: "compute", recommendationType: "tighten"})
	require.NoError(t, err)
	assert.Equal(t, "tighten", got.RecommendationType)
}

func TestSelectGPUTimeslicingRec(t *testing.T) {
	recs := []gpu.TimeslicingRec{
		{NodeName: "gpu-1", GPUModel: "A100", Term: "short", RecommendedReplicas: 4},
		{NodeName: "gpu-1", GPUModel: "H100", Term: "short", RecommendedReplicas: 2},
	}
	_, err := selectGPUTimeslicingRec(recs, explainFlags{node: "gpu-1", gpuModel: "missing", term: "short"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching gpu timeslicing")
	got, err := selectGPUTimeslicingRec(recs, explainFlags{node: "gpu-1", gpuModel: "H100", term: "short"})
	require.NoError(t, err)
	assert.Equal(t, 2, got.RecommendedReplicas)
}

func TestToGPUTimeslicingExplainOut_SnakeCase(t *testing.T) {
	out := toGPUTimeslicingExplainOut(gpu.TimeslicingRec{
		NodeName: "gpu-1", GPUModel: "A100", Term: "short", RecommendedReplicas: 4,
		Expl: types.NodeGPUTimeslicingExplanationFactors{DataDays: 1, CandidateCount: 2, ClassificationRule: "idle"},
	})
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"classification_rule"`)
	assert.NotContains(t, string(raw), "ClassificationRule")
	assert.Equal(t, 2, out.CandidateCount)
}

func TestToVMExplainOut_UsesExplHelper(t *testing.T) {
	days := 4
	branch := "cost_floor"
	out := toVMExplainOut(vm.VMRecommendation{Namespace: "ns", VMName: "vm", Term: "short_term", Engine: "cost", ExplDataDays: &days, ExplSizingBranch: &branch})
	assert.Equal(t, 4, out.DataDays)
	assert.Equal(t, "cost_floor", out.SizingBranch)
}

func TestToSnapshotExplainOut_ExplFields(t *testing.T) {
	out := toSnapshotExplainOut(snapshot.SnapshotRec{
		Namespace: "app", SnapshotName: "snap-a", RecommendationType: "orphan",
		Expl: types.SnapshotExplanationFactors{ThresholdUsed: 30, ThresholdName: "orphan_age_days", ClassificationRule: "orphan"},
	})
	assert.Equal(t, 30, out.ThresholdUsed)
	assert.Equal(t, "orphan", out.ClassificationRule)
}

func TestToNodeExplainOut_SnakeCase(t *testing.T) {
	out := toNodeExplainOut(node.Rec{Node: "n1", Term: "short", Engine: "cost", Expl: types.NodeExplanationFactors{DataDays: 1, SizingFormula: "cost"}})
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"sizing_formula"`)
	assert.NotContains(t, string(raw), "SizingFormula")
}
