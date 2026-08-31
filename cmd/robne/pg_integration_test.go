package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modeltypes "github.com/redhatinsights/ros-ocp-backend/internal/model/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgdigest"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

func TestEmbeddedHeadPositive(t *testing.T) {
	t.Parallel()
	head, err := embeddedHead()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, head, uint(185))
}

func TestNativeContainerIDMatchesModel(t *testing.T) {
	t.Parallel()
	got := pgrec.NativeContainerID(testutil.TestClusterUUID, "ns", "wl", "deployment", "main")
	want := modeltypes.NativeContainerID(testutil.TestClusterUUID, "ns", "wl", "deployment", "main")
	assert.Equal(t, want, got)
}

func TestNativeEntityIDsMatchModel(t *testing.T) {
	t.Parallel()
	c := testutil.TestClusterUUID
	assert.Equal(t, modeltypes.NativeNamespaceID(c, "ns"), pgrec.NativeNamespaceID(c, "ns"))
	assert.Equal(t, modeltypes.NativeNodeID(c, "worker"), pgrec.NativeNodeID(c, "worker"))
	assert.Equal(t, modeltypes.NativePvcID(c, "ns", "data"), pgrec.NativePvcID(c, "ns", "data"))
	assert.Equal(t, modeltypes.NativeQuotaID(c, "ns", "compute"), pgrec.NativeQuotaID(c, "ns", "compute"))
	assert.Equal(t, modeltypes.NativeClusterQuotaID(c, "team"), pgrec.NativeClusterQuotaID(c, "team"))
	assert.Equal(t, modeltypes.NativeVMID(c, "ns", "vm-1"), pgrec.NativeVMID(c, "ns", "vm-1"))
}

func TestExecuteRecommend_PathBUsesStoredHistory(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)

	var hist []types.KeyedDigest
	for i := 1; i <= 6; i++ {
		d := sampleDigest()
		d.Row.BucketDate = time.Date(2026, 8, i, 0, 0, 0, 0, time.UTC)
		hist = append(hist, d)
	}
	require.NoError(t, pgdigest.WriteContainerDigests(ctx, pool, orgID, cluster, hist))

	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(dayCSV("app", "api", cluster, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := executeRecommend(commonFlags{
		input:        csvPath,
		output:       poolDSN(t, pool, ""),
		noUserConfig: true,
		now:          "2026-08-07T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	var medium *types.ContainerRec
	for i := range result.Recs {
		if result.Recs[i].Term == "medium" && result.Recs[i].Engine == "cost" {
			medium = &result.Recs[i]
			break
		}
	}
	require.NotNil(t, medium, "expected medium/cost rec")
	assert.GreaterOrEqual(t, medium.DataDays, 7, "path B must SELECT stored days plus today, not CSV-only")
}

func TestExecuteRecommend_PathARecompute(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	var hist []types.KeyedDigest
	for i := 1; i <= 7; i++ {
		d := sampleDigest()
		d.Row.BucketDate = time.Date(2026, 8, i, 0, 0, 0, 0, time.UTC)
		hist = append(hist, d)
	}
	require.NoError(t, pgdigest.WriteContainerDigests(ctx, pool, orgID, cluster, hist))
	require.NoError(t, pgrec.EnsureAccountCluster(ctx, pool, orgID, cluster, time.Date(2026, 8, 7, 2, 0, 0, 0, time.UTC)))

	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	dsn := poolDSN(t, pool, "")
	result, err := executeRecommend(commonFlags{
		input:        dsn,
		noUserConfig: true,
		now:          "2026-08-07T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Recs)
	var medium *types.ContainerRec
	for i := range result.Recs {
		if result.Recs[i].Term == "medium" && result.Recs[i].Engine == "cost" {
			medium = &result.Recs[i]
			break
		}
	}
	require.NotNil(t, medium)
	assert.GreaterOrEqual(t, medium.DataDays, 7)
}

func pathAEnv(t *testing.T, orgID, cluster string) {
	t.Helper()
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)
}

func TestExecuteRecommend_PathANamespaceRecompute(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceQuotaTwoDayCSV("app", "compute")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := executeRecommend(commonFlags{
		input:        csvPath,
		output:       poolDSN(t, pool, ""),
		plugins:      "namespace",
		noUserConfig: true,
		now:          "2026-08-02T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)

	result, err := executeRecommend(commonFlags{
		input:        poolDSN(t, pool, ""),
		plugins:      "namespace",
		noUserConfig: true,
		now:          "2026-08-02T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.NamespaceRecs)
	assert.Empty(t, result.Recs)
}

func TestExecuteRecommend_PathANamespaceEmptySelectErrors(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	pathAEnv(t, "1234567", testutil.TestClusterUUID)
	_, err := executeRecommend(commonFlags{
		input:        poolDSN(t, pool, ""),
		plugins:      "namespace",
		noUserConfig: true,
		now:          "2026-08-07T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace digest")
}

func TestExecuteRecommend_PathAMixedContainerNamespace(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", cluster)), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayCSV("app", "compute")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := executeRecommend(commonFlags{
		input:        cwd,
		output:       poolDSN(t, pool, ""),
		plugins:      "container,namespace",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)

	result, err := executeRecommend(commonFlags{
		input:        poolDSN(t, pool, ""),
		plugins:      "container,namespace",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Recs)
	require.NotEmpty(t, result.NamespaceRecs)
}

func TestExecuteRecommend_PathAQuotaNestedNoContainerPlugin(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", cluster)), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayCSV("app", "compute")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := executeRecommend(commonFlags{
		input:        cwd,
		output:       poolDSN(t, pool, ""),
		plugins:      "quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)

	result, err := executeRecommend(commonFlags{
		input:        poolDSN(t, pool, ""),
		output:       poolDSN(t, pool, ""),
		plugins:      "quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.QuotaRecs)

	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2`, orgID, cluster).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestExecuteRecommend_PathAClusterQuotaWithoutQuotaDays(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, persistRecommendations(ctx, commonFlags{output: poolDSN(t, pool, "")}, recommendResult{
		OrgID:     orgID,
		ClusterID: cluster,
		Now:       day,
		ClusterQuotaDigests: []quota.ClusterQuotaSnapshot{{
			ClusterQuotaName: "team-a", CPURequestHardMC: 10000, CPURequestUsedMC: 1000, LastObservedAt: day,
		}},
	}))
	pathAEnv(t, orgID, cluster)

	result, err := executeRecommend(commonFlags{
		input:        poolDSN(t, pool, ""),
		plugins:      "cluster_quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.ClusterQuotaRecs)
	assert.Empty(t, result.QuotaRecs)
	assert.Empty(t, result.Recs)
}

func TestExecuteRecommend_PathANodeGPUPVCVM(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	alloc := int64(4000)
	require.NoError(t, persistRecommendations(ctx, commonFlags{output: poolDSN(t, pool, "")}, recommendResult{
		OrgID:     orgID,
		ClusterID: cluster,
		Now:       day,
		NodeDigests: []node.DigestRow{{
			BucketDate: day, Node: "worker-1", CPUUsageP95MC: 200, CPUUsageMaxMC: 250, SampleCount: 24, MaxCPUAllocMC: &alloc,
		}},
		GPUDigests: map[gpu.GPUContainerKey][]gpu.GPUDigestRow{
			{Namespace: "ml", Workload: "train", ContainerName: "gpu-worker"}: {{
				IntervalStart: day, GPUModelName: "NVIDIA A100-SXM4-80GB", NodeName: "gpu-1",
				FBUsageMaxMiB: 12000, GPUCount: 1,
			}},
		},
		PVCDigests: map[pvc.PVCKey][]pvc.PVCDigestRow{
			{Namespace: "production", PVC: "data-pvc"}: {{
				BucketDate: day, Namespace: "production", PVC: "data-pvc",
				CapacityBytes: 100 << 30, RequestBytes: 80 << 30, UsageBytesMin: 10, UsageBytesMax: 20 << 30, UsageBytesAvg: 15, SampleCount: 24,
			}},
		},
		VMDigests: []vm.DailyVMDigest{{
			VMName: "web-vm", Namespace: "vms", BucketDate: day, NodeName: "worker-1",
			CPUUsageP95MC: 1500, CPURequestMC: 2000, SampleCount: 24,
			Devices: []vm.GPUDeviceDigest{{UUID: "GPU-aaa", Model: "A100", UtilAvgBP: 1000}},
		}},
	}))
	pathAEnv(t, orgID, cluster)

	nodeOut, err := executeRecommend(commonFlags{
		input: poolDSN(t, pool, ""), plugins: "node", noUserConfig: true, now: "2026-08-01T02:00:00Z", format: "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, nodeOut.NodeRecs)

	gpuOut, err := executeRecommend(commonFlags{
		input: poolDSN(t, pool, ""), plugins: "gpu", noUserConfig: true, now: "2026-08-01T02:00:00Z", format: "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, gpuOut.GPURecs)

	pvcOut, err := executeRecommend(commonFlags{
		input: poolDSN(t, pool, ""), plugins: "pvc", noUserConfig: true, now: "2026-08-01T02:00:00Z", format: "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, pvcOut.PVCRecs)

	vmOut, err := executeRecommend(commonFlags{
		input: poolDSN(t, pool, ""), plugins: "vm", noUserConfig: true, now: "2026-08-01T02:00:00Z", format: "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, vmOut.VMRecs)
	assert.Equal(t, "web-vm", vmOut.VMRecs[0].VMName)
}

func TestExecuteRecommend_EmptySelectErrors(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, "1234567", testutil.TestClusterUUID)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := executeRecommend(commonFlags{
		input:        poolDSN(t, pool, ""),
		noUserConfig: true,
		now:          "2026-08-07T02:00:00Z",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest")
}

func TestPersist_RefuseForeignSourceID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, "1234567")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, source_id, cluster_uuid, cluster_alias, last_reported_at)
		SELECT id, 'sources-uuid', $1, 'helm', now() FROM rh_accounts WHERE org_id = $2`,
		testutil.TestClusterUUID, "1234567")
	require.NoError(t, err)

	err = persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, sampleResult())
	require.Error(t, err)
	assert.ErrorIs(t, err, pgrec.ErrForeignSourceID)
}

func TestPersist_UpsertContainerRec(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	result := sampleResult()
	err := persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, result)
	require.NoError(t, err)

	var n int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var sourceID string
	err = pool.QueryRow(ctx, `
		SELECT c.source_id FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2`,
		result.OrgID, result.ClusterID).Scan(&sourceID)
	require.NoError(t, err)
	assert.Equal(t, pgrec.SourceID, sourceID)

	result.Recs[0].RecCPURequestMC = 99
	require.NoError(t, persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, result))
	var cpu int64
	err = pool.QueryRow(ctx, `
		SELECT rec_cpu_request_millicores FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&cpu)
	require.NoError(t, err)
	assert.Equal(t, int64(99), cpu)
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestExecuteRecommend_PathBPersistsNamespaceRecs(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceOneDayCSV("kube-system")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := executeRecommend(commonFlags{
		input:        csvPath,
		output:       poolDSN(t, pool, ""),
		plugins:      "namespace",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.NamespaceRecs)
	assert.Empty(t, result.Recs)

	var n int
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM namespace_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = $3`,
		orgID, cluster, "kube-system").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, len(result.NamespaceRecs), n)

	var digestN int
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND schedule_type = 'all_hours'`,
		orgID, cluster, "kube-system").Scan(&digestN)
	require.NoError(t, err)
	assert.Equal(t, 1, digestN)
}

func TestPersist_MixedEntityRecs(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	clusterUUID := uuid.MustParse(cluster)
	now := time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC)
	inst := "u1.xlarge"
	result := recommendResult{
		OrgID:      orgID,
		ClusterID:  cluster,
		Now:        now,
		plugins:    []string{"container", "namespace", "node", "gpu", "pvc", "vm", "quota", "cluster_quota"},
		ValidTerms: []string{"short", "medium", "long"},
		Recs: []types.ContainerRec{{
			OrgID: orgID, ClusterUUID: cluster, Namespace: "app", Workload: "api",
			WorkloadType: "deployment", ContainerName: "api", Term: "short", Engine: "cost",
			RecCPURequestMC: 50, RecCPULimitMC: 60, RecMemRequestKiB: 1024, RecMemLimitKiB: 2048,
		}},
		NamespaceRecs: []namespace.NamespaceRec{{
			OrgID: orgID, ClusterUUID: cluster, Namespace: "kube-system",
			Term: "short", Engine: "cost", RecCPURequestMC: 100, RecCPULimitMC: 200,
			RecMemRequestKiB: 4096, RecMemLimitKiB: 8192,
		}},
		NodeRecs: []node.Rec{{
			Node: "worker-1", Term: "short", Engine: "cost", Category: "underutilized",
			IdleState: types.IdleStateActive, RecommendedCPUMC: 1000, RecommendedMemKiB: 2048,
			NotificationCodes: []int16{},
		}},
		GPURecs: []gpuRecRow{{
			Namespace: "ml", Workload: "train", ContainerName: "gpu-worker", NodeName: "gpu-1",
			Rec: gpu.GPURec{
				Term: "short", GPUModelName: "NVIDIA A100-SXM4-80GB",
				RecommendedGPUProfile: "3g.40gb", CurrentGPUProfile: "full_gpu",
				Classification: gpu.GPUClassUnderutilized, Confidence: 0.8, FBUsageMaxMiB: 12000,
			},
		}},
		GPUTimeslicing: []gpu.TimeslicingRec{{
			NodeName: "gpu-1", ClusterUUID: cluster, GPUModel: "NVIDIA A100-SXM4-80GB",
			Term: "short", RecommendedReplicas: 2, Confidence: 0.7,
			NotificationCodes:   []int16{},
			CandidateContainers: []gpu.GPUContainerRef{{Namespace: "ml", Workload: "train", Container: "gpu-worker"}},
		}},
		PVCRecs: []pvc.PVCRec{{
			OrgID: orgID, ClusterUUID: cluster, Namespace: "production", PVC: "data-pvc",
			StorageClass: "gp3", CapacityBytes: 100 << 30, UsageBytesMax: 10 << 30,
			UsageRatio: 0.1, RecommendationType: pvc.PVCRecTypeOversized, DataDays: 2, Term: "short",
		}},
		VMRecs: []vm.VMRecommendation{{
			OrgID: orgID, ClusterUUID: clusterUUID, VMName: "web-vm", Namespace: "vms",
			Term: "short", Engine: "cost", RecommendedVCPU: 2, RecommendedMemoryGiB: 8,
			Confidence: "high", LastRecommendedAt: now, RecommendedInstanceType: &inst,
		}},
		QuotaRecs: []quota.QuotaRec{{
			OrgID: orgID, ClusterUUID: cluster, Namespace: "app", QuotaName: "compute-resources",
			HeadroomBP: 11000, RecommendationType: quota.QuotaRecTypeTighten, RiskLevel: quota.QuotaRiskLow,
			Currency: "USD", NotificationCodes: []int16{},
			Snapshot:    quota.NamespaceQuotaSnapshot{CPURequestHardMC: 100000, CPURequestUsedMC: 25000, LastObservedAt: now},
			Recommended: quota.QuotaResourceBundle{CPURequestMillicores: 36000},
		}},
		ClusterQuotaRecs: []quota.ClusterQuotaRec{{
			OrgID: orgID, ClusterUUID: cluster, ClusterQuotaName: "team-a",
			RecommendationType: quota.QuotaRecTypeOptimal, RiskLevel: quota.QuotaRiskLow,
			NotificationCodes: []int16{},
			Snapshot:          quota.ClusterQuotaSnapshot{CPURequestHardMC: 10000},
			Recommended:       quota.QuotaResourceBundle{CPURequestMillicores: 3300},
		}},
	}
	require.NoError(t, persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, result))

	assertCount := func(sql string, want int) {
		t.Helper()
		var n int
		require.NoError(t, pool.QueryRow(ctx, sql, orgID, cluster).Scan(&n))
		assert.Equal(t, want, n, sql)
	}
	assertCount(`SELECT COUNT(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, 1)
	assertCount(`SELECT COUNT(*) FROM namespace_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, 1)
	assertCount(`SELECT COUNT(*) FROM node_recommendations WHERE org_id = $1 AND cluster_uuid = $2`, 1)
	assertCount(`SELECT COUNT(*) FROM gpu_mig_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, 1)
	assertCount(`SELECT COUNT(*) FROM node_gpu_timeslicing_recommendations WHERE org_id = $1 AND cluster_uuid = $2`, 1)
	assertCount(`SELECT COUNT(*) FROM pvc_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, 1)
	assertCount(`SELECT COUNT(*) FROM vm_recommendations WHERE org_id = $1 AND cluster_uuid = $2`, 1)
	assertCount(`SELECT COUNT(*) FROM quota_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, 1)
	assertCount(`SELECT COUNT(*) FROM cluster_quota_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, 1)
}

func TestPersist_GPURecsWithoutProductQuery(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	result := recommendResult{
		OrgID:      orgID,
		ClusterID:  cluster,
		Now:        time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC),
		plugins:    []string{"gpu"},
		ValidTerms: []string{"short"},
		GPURecs: []gpuRecRow{{
			Namespace: "ml", Workload: "train", ContainerName: "gpu-worker", NodeName: "gpu-1",
			Rec: gpu.GPURec{
				Term: "short", GPUModelName: "NVIDIA A100-SXM4-80GB",
				RecommendedGPUProfile: "3g.40gb", Classification: gpu.GPUClassUnderutilized,
				Confidence: 0.9, FBUsageMaxMiB: 8000,
			},
		}},
	}
	require.NoError(t, persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, result))

	var profile, nodeName string
	err := pool.QueryRow(ctx, `
		SELECT recommended_gpu_profile, node_name FROM gpu_mig_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND workload = $4 AND container_name = $5 AND term = $6`,
		orgID, cluster, "ml", "train", "gpu-worker", "short",
	).Scan(&profile, &nodeName)
	require.NoError(t, err)
	assert.Equal(t, "3g.40gb", profile)
	assert.Equal(t, "gpu-1", nodeName)

	var tsCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_gpu_timeslicing_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2`, orgID, cluster).Scan(&tsCount))
	assert.Equal(t, 0, tsCount)
}

func TestExecuteRecommend_PathBQuotaPersistsAllQuotaDays(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	csvPath := filepath.Join(cwd, "ocp_ros_namespace_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(namespaceQuotaTwoDayCSV("app", "compute")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := executeRecommend(commonFlags{
		input:        csvPath,
		output:       poolDSN(t, pool, ""),
		plugins:      "quota",
		noUserConfig: true,
		now:          "2026-08-02T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.QuotaRecs)
	assert.Empty(t, result.NamespaceRecs)

	var quotaDays, nsDays int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM daily_namespace_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND quota_name = $4`,
		orgID, cluster, "app", "compute").Scan(&quotaDays))
	assert.Equal(t, 2, quotaDays)
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		orgID, cluster, "app").Scan(&nsDays))
	assert.Equal(t, 2, nsDays)
}

func TestExecuteRecommend_PathBQuotaWithContainerCSVPersistsContainerDays(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_usage.csv"), []byte(oneDayCSV("app", "api", cluster)), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "ocp_ros_namespace_usage.csv"), []byte(namespaceQuotaOneDayCSV("app", "compute")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	result, err := executeRecommend(commonFlags{
		input:        cwd,
		output:       poolDSN(t, pool, ""),
		plugins:      "quota",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Recs)
	require.NotEmpty(t, result.QuotaRecs)

	var containers, namespaces int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, cluster).Scan(&containers))
	assert.Equal(t, 1, containers)
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM daily_namespace_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, cluster).Scan(&namespaces))
	assert.Equal(t, 1, namespaces)
}

func TestExecuteRecommend_PathBGPUPersistsGPUDigests(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	cwd := t.TempDir()
	writeRobneYAML(t, cwd, orgID, cluster)
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayGPUCSV("ml", "train", cluster, "gpu-1")), 0o600))

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	_, err := executeRecommend(commonFlags{
		input:        csvPath,
		output:       poolDSN(t, pool, ""),
		plugins:      "gpu",
		noUserConfig: true,
		now:          "2026-08-01T02:00:00Z",
		format:       "json",
	})
	require.NoError(t, err)

	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM gpu_container_digests
		WHERE cluster_uuid = $1 AND namespace = $2 AND workload = $3 AND container_name = $4`,
		cluster, "ml", "train", "train").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestPersist_MixedEntityDigests(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	day := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	alloc := int64(4000)
	result := recommendResult{
		OrgID:     orgID,
		ClusterID: cluster,
		Now:       day,
		NamespaceDigests: map[namespace.NamespaceKey][]types.DigestRow{
			{Namespace: "app"}: {{BucketDate: day, CPUUsageP95MC: 100, SampleCount: 24}},
		},
		NodeDigests: []node.DigestRow{{
			BucketDate: day, Node: "worker-1", CPUUsageP95MC: 200, CPUUsageMaxMC: 250, SampleCount: 24, MaxCPUAllocMC: &alloc,
		}},
		GPUDigests: map[gpu.GPUContainerKey][]gpu.GPUDigestRow{
			{Namespace: "ml", Workload: "train", ContainerName: "gpu-worker"}: {{
				IntervalStart: day, GPUModelName: "NVIDIA A100-SXM4-80GB", NodeName: "gpu-1",
				FBUsageMaxMiB: 12000, GPUCount: 1,
			}},
		},
		PVCDigests: map[pvc.PVCKey][]pvc.PVCDigestRow{
			{Namespace: "production", PVC: "data-pvc"}: {{
				BucketDate: day, Namespace: "production", PVC: "data-pvc",
				CapacityBytes: 100, RequestBytes: 80, UsageBytesMin: 10, UsageBytesMax: 20, UsageBytesAvg: 15, SampleCount: 24,
			}},
		},
		VMDigests: []vm.DailyVMDigest{{
			VMName: "web-vm", Namespace: "vms", BucketDate: day, NodeName: "worker-1",
			CPUUsageP95MC: 1500, SampleCount: 24, HasGPU: true, GPUCount: 1, GPUModel: "A100",
			Devices: []vm.GPUDeviceDigest{{UUID: "GPU-aaa", Model: "A100", UtilAvgBP: 1000}},
		}},
		QuotaDigests: []quota.NamespaceQuotaSnapshot{{
			Namespace: "app", QuotaName: "compute", CPURequestHardMC: 2000, LastObservedAt: day,
		}},
		ClusterQuotaDigests: []quota.ClusterQuotaSnapshot{{
			ClusterQuotaName: "team-a", CPURequestHardMC: 10000, Namespaces: "app", LastObservedAt: day,
		}},
	}
	require.NoError(t, persistRecommendations(ctx, commonFlags{output: poolDSN(t, pool, "")}, result))

	assertCount := func(sql string, args []any, want int) {
		t.Helper()
		var n int
		require.NoError(t, pool.QueryRow(ctx, sql, args...).Scan(&n))
		assert.Equal(t, want, n, sql)
	}
	assertCount(`SELECT COUNT(*) FROM daily_namespace_digests WHERE org_id = $1 AND cluster_uuid = $2`, []any{orgID, cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM daily_node_digests WHERE org_id = $1 AND cluster_uuid = $2`, []any{orgID, cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM gpu_container_digests WHERE cluster_uuid = $1`, []any{cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM daily_pvc_digests WHERE cluster_uuid = $1`, []any{cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM daily_vm_digests WHERE org_id = $1 AND cluster_uuid = $2`, []any{orgID, cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM vm_gpu_device_digests WHERE gpu_uuid = $1`, []any{"GPU-aaa"}, 1)
	assertCount(`SELECT COUNT(*) FROM daily_namespace_quota_digests WHERE org_id = $1 AND cluster_uuid = $2`, []any{orgID, cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM daily_cluster_quota_digests WHERE org_id = $1 AND cluster_uuid = $2`, []any{orgID, cluster}, 1)

	assert.True(t, relExists(t, pool, "daily_namespace_digests_202401"))
	assert.True(t, relExists(t, pool, "daily_node_digests_202401"))
	assert.True(t, relExists(t, pool, "gpu_container_digests_202401"))
	assert.True(t, relExists(t, pool, "daily_pvc_digests_202401"))
	assert.False(t, relExists(t, pool, "daily_namespace_quota_digests_202401"))
	assert.False(t, relExists(t, pool, "daily_cluster_quota_digests_202401"))
	assert.False(t, relExists(t, pool, "daily_vm_digests_202401"))
}

func TestPersist_BusinessHoursPluginDigests(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	day := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	alloc := int64(4000)
	nodeRow := node.DigestRow{
		BucketDate: day, Node: "worker-1", CPUUsageP95MC: 200, CPUUsageMaxMC: 250, SampleCount: 24, MaxCPUAllocMC: &alloc,
	}
	gpuKey := gpu.GPUContainerKey{Namespace: "ml", Workload: "train", ContainerName: "gpu-worker"}
	gpuRow := gpu.GPUDigestRow{
		IntervalStart: day, GPUModelName: "NVIDIA A100-SXM4-80GB", NodeName: "gpu-1",
		FBUsageMaxMiB: 12000, GPUCount: 1,
	}
	vmRow := vm.DailyVMDigest{
		VMName: "web-vm", Namespace: "vms", BucketDate: day, NodeName: "worker-1",
		CPUUsageP95MC: 1500, SampleCount: 24,
	}
	require.NoError(t, persistRecommendations(ctx, commonFlags{output: poolDSN(t, pool, "")}, recommendResult{
		OrgID:     orgID,
		ClusterID: cluster,
		Now:       day,
		NodeDigests: []node.DigestRow{nodeRow},
		GPUDigests: map[gpu.GPUContainerKey][]gpu.GPUDigestRow{
			gpuKey: {gpuRow},
		},
		VMDigests:    []vm.DailyVMDigest{vmRow},
		BHNodeDigests: []node.DigestRow{nodeRow},
		BHGPUDigests: map[gpu.GPUContainerKey][]gpu.GPUDigestRow{
			gpuKey: {gpuRow},
		},
		BHVMDigests: []vm.DailyVMDigest{vmRow},
		BHNodeRecs: []node.Rec{{
			Node: "worker-1", Term: "short", Engine: "cost", Category: "underutilized",
		}},
	}))

	assertCount := func(sql string, args []any, want int) {
		t.Helper()
		var n int
		require.NoError(t, pool.QueryRow(ctx, sql, args...).Scan(&n))
		assert.Equal(t, want, n, sql)
	}
	assertCount(`SELECT COUNT(*) FROM daily_node_digests WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'all_hours'`, []any{orgID, cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM daily_node_digests WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'business_hours'`, []any{orgID, cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM gpu_container_digests WHERE cluster_uuid = $1 AND schedule_type = 'all_hours'`, []any{cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM gpu_container_digests WHERE cluster_uuid = $1 AND schedule_type = 'business_hours'`, []any{cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM daily_vm_digests WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'all_hours'`, []any{orgID, cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM daily_vm_digests WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'business_hours'`, []any{orgID, cluster}, 1)
	assertCount(`SELECT COUNT(*) FROM node_recommendations WHERE org_id = $1 AND cluster_uuid = $2`, []any{orgID, cluster}, 0)
}

// Pins #505 GREATEST/LEAST plus #508: a second write with a different org_id
// does not rewrite the existing tenant (unique key has no org_id).
func TestPersist_PVCMergeKeepsMaxUsage(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	write := func(org, pod string, minBytes, maxBytes, avgBytes int64, samples int, capBytes int64) {
		t.Helper()
		require.NoError(t, persistRecommendations(ctx, commonFlags{output: poolDSN(t, pool, "")}, recommendResult{
			OrgID:     org,
			ClusterID: cluster,
			Now:       day,
			PVCDigests: map[pvc.PVCKey][]pvc.PVCDigestRow{
				{Namespace: "production", PVC: "data-pvc"}: {{
					BucketDate: day, Namespace: "production", PVC: "data-pvc",
					CapacityBytes: capBytes, RequestBytes: 80,
					UsageBytesMin: minBytes, UsageBytesMax: maxBytes, UsageBytesAvg: avgBytes,
					SampleCount: samples, LastSeenPod: pod,
				}},
			},
		}))
	}
	write(orgID, "pod-a", 10, 50, 50, 2, 100)
	write(orgID, "", 1, 10, 10, 2, 90)
	var n int
	var gotOrg, gotPod string
	var gotMin, gotMax, gotCap, gotCount, gotAvg int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*), MIN(org_id), MIN(last_seen_pod),
			MIN(usage_bytes_min), MAX(usage_bytes_max), MAX(capacity_bytes),
			MAX(sample_count), MAX(usage_bytes_avg)
		FROM daily_pvc_digests
		WHERE cluster_uuid = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		cluster, "production", "data-pvc").Scan(&n, &gotOrg, &gotPod, &gotMin, &gotMax, &gotCap, &gotCount, &gotAvg))
	assert.Equal(t, 1, n)
	assert.Equal(t, orgID, gotOrg)
	assert.Equal(t, "pod-a", gotPod)
	assert.Equal(t, int64(1), gotMin)
	assert.Equal(t, int64(50), gotMax)
	assert.Equal(t, int64(100), gotCap)
	assert.Equal(t, int64(4), gotCount)
	assert.Equal(t, int64(30), gotAvg)

	write("9999999", "pod-b", 1, 10, 10, 2, 90)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id, last_seen_pod FROM daily_pvc_digests
		WHERE cluster_uuid = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		cluster, "production", "data-pvc").Scan(&gotOrg, &gotPod))
	assert.Equal(t, orgID, gotOrg)
	assert.Equal(t, "pod-b", gotPod)
}

func TestPersist_QuotaMergeKeepsGreatestUsed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	write := func(used, hard int64, nsList string) {
		t.Helper()
		require.NoError(t, persistRecommendations(ctx, commonFlags{output: poolDSN(t, pool, "")}, recommendResult{
			OrgID:     orgID,
			ClusterID: cluster,
			Now:       day,
			QuotaDigests: []quota.NamespaceQuotaSnapshot{{
				Namespace: "app", QuotaName: "compute",
				CPURequestUsedMC: used, CPURequestHardMC: hard, LastObservedAt: day,
			}},
			ClusterQuotaDigests: []quota.ClusterQuotaSnapshot{{
				ClusterQuotaName: "team-a", CPURequestUsedMC: used, CPURequestHardMC: hard,
				Namespaces: nsList, LastObservedAt: day,
			}},
		}))
	}
	write(100, 2000, "app")
	write(50, 3000, "")
	var nsUsed, nsHard, crqUsed, crqHard int64
	var crqNS string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT cpu_request_used, cpu_request_hard FROM daily_namespace_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND quota_name = $4`,
		orgID, cluster, "app", "compute").Scan(&nsUsed, &nsHard))
	assert.Equal(t, int64(100), nsUsed)
	assert.Equal(t, int64(3000), nsHard)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT cpu_request_used, cpu_request_hard, namespaces FROM daily_cluster_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND cluster_quota_name = $3`,
		orgID, cluster, "team-a").Scan(&crqUsed, &crqHard, &crqNS))
	assert.Equal(t, int64(100), crqUsed)
	assert.Equal(t, int64(3000), crqHard)
	assert.Equal(t, "app", crqNS)
}

func TestPersist_GPUDigestsWithoutProductQuery(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, persistRecommendations(ctx, commonFlags{output: poolDSN(t, pool, "")}, recommendResult{
		OrgID:     orgID,
		ClusterID: cluster,
		Now:       day,
		GPUDigests: map[gpu.GPUContainerKey][]gpu.GPUDigestRow{
			{Namespace: "ml", Workload: "train", ContainerName: "gpu-worker"}: {{
				IntervalStart: day, GPUModelName: "NVIDIA A100-SXM4-80GB", NodeName: "gpu-1",
				FBUsageAvgMiB: 8000, GPUCount: 2,
			}},
		},
	}))
	var fb float64
	var count int
	err := pool.QueryRow(ctx, `
		SELECT fb_usage_avg_mib, gpu_count FROM gpu_container_digests
		WHERE cluster_uuid = $1 AND namespace = $2 AND workload = $3 AND container_name = $4`,
		cluster, "ml", "train", "gpu-worker").Scan(&fb, &count)
	require.NoError(t, err)
	assert.Equal(t, float64(8000), fb)
	assert.Equal(t, 2, count)
}

func relExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM pg_class WHERE relname = $1`, name).Scan(&n))
	return n > 0
}

func TestOpenPostgres_EmptyRequiresApplySchema(t *testing.T) {
	admin := testutil.SetupTestDB(t)
	dsn := createEmptyDatabase(t, admin)
	_, err := openPostgres(context.Background(), dsn, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--apply-schema")
	assert.Contains(t, err.Error(), "empty")
}

func TestOpenPostgres_EmptyWithApplySchema(t *testing.T) {
	admin := testutil.SetupTestDB(t)
	dsn := createEmptyDatabase(t, admin)
	pool, err := openPostgres(context.Background(), dsn, true)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	var n int
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM recommendation_sets`).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestOpenPostgres_BehindRequiresApplySchema(t *testing.T) {
	admin := testutil.SetupTestDB(t)
	dsn := createEmptyDatabase(t, admin)
	m, err := newEmbeddedMigrate(dsn)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(1))
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	_, err = openPostgres(context.Background(), dsn, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--apply-schema")
	assert.Contains(t, err.Error(), "1")

	upgraded, err := openPostgres(context.Background(), dsn, true)
	require.NoError(t, err)
	t.Cleanup(upgraded.Close)
	var n int
	err = upgraded.QueryRow(context.Background(), `SELECT COUNT(*) FROM recommendation_sets`).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestOpenPostgres_Dirty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_, err := pool.Exec(context.Background(), `UPDATE schema_migrations SET dirty = true`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE schema_migrations SET dirty = false`)
	})
	_, err = openPostgres(context.Background(), poolDSN(t, pool, ""), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dirty")
}

func TestOpenPostgres_NewerThanBinary(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_, err := pool.Exec(context.Background(), `UPDATE schema_migrations SET version = 999999`)
	require.NoError(t, err)
	t.Cleanup(func() {
		head, herr := embeddedHead()
		require.NoError(t, herr)
		_, _ = pool.Exec(context.Background(), `UPDATE schema_migrations SET version = $1`, head)
	})
	_, err = openPostgres(context.Background(), poolDSN(t, pool, ""), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer")
}

func TestPersist_WritesDigestsThenRecs(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	result := sampleResult()
	result.OrgID = "robne-pgdigest-cli"
	result.Digests = []types.KeyedDigest{sampleDigest()}
	result.Recs[0].OrgID = result.OrgID

	err := persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, result)
	require.NoError(t, err)

	var n int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = $3::digest_schedule_type`,
		result.OrgID, result.ClusterID, pgdigest.ScheduleAllHours).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	result.Digests[0].Row.SampleCount = 99
	result.Recs[0].RecCPURequestMC = 77
	require.NoError(t, persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, result))

	var samples, cpu int64
	err = pool.QueryRow(ctx, `
		SELECT sample_count FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&samples)
	require.NoError(t, err)
	assert.Equal(t, int64(99), samples)
	err = pool.QueryRow(ctx, `
		SELECT rec_cpu_request_millicores FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&cpu)
	require.NoError(t, err)
	assert.Equal(t, int64(77), cpu)
}

func TestPgdigest_ReadRoundTrip(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "robne-pgdigest-read"
	in := sampleDigest()
	cv := int64(1200)
	in.Row.CPUUsageCVBP = &cv
	in.Row.CPURequestP50MC = 50
	require.NoError(t, pgdigest.WriteContainerDigests(ctx, pool, orgID, testutil.TestClusterUUID, []types.KeyedDigest{in}))

	bh := sampleDigest()
	bh.Row.BucketDate = in.Row.BucketDate
	require.NoError(t, pgdigest.WriteRows(ctx, pool, []pgdigest.Row{{
		OrgID:        orgID,
		ClusterUUID:  testutil.TestClusterUUID,
		ScheduleType: "business_hours",
		Digest:       bh,
	}}))

	other := sampleDigest()
	require.NoError(t, pgdigest.WriteContainerDigests(ctx, pool, "other-org", testutil.TestClusterUUID, []types.KeyedDigest{other}))

	old := sampleDigest()
	old.Row.BucketDate = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, pgdigest.WriteContainerDigests(ctx, pool, orgID, testutil.TestClusterUUID, []types.KeyedDigest{old}))

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	got, err := pgdigest.ReadContainerDigests(ctx, pool, orgID, testutil.TestClusterUUID, start, end)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, in.Key, got[0].Key)
	assert.Equal(t, in.Row.BucketDate.UTC().Format("2006-01-02"), got[0].Row.BucketDate.UTC().Format("2006-01-02"))
	assert.Equal(t, int64(50), got[0].Row.CPURequestP50MC)
	require.NotNil(t, got[0].Row.CPUUsageCVBP)
	assert.Equal(t, cv, *got[0].Row.CPUUsageCVBP)
}

func TestPgdigest_CreatesHistoricalPartition(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "robne-pgdigest-hist"
	d := sampleDigest()
	d.Row.BucketDate = time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)
	err := pgdigest.WriteContainerDigests(ctx, pool, orgID, testutil.TestClusterUUID, []types.KeyedDigest{d})
	require.NoError(t, err)

	var n int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM daily_container_digests
		WHERE org_id = $1 AND bucket_date = $2`,
		orgID, d.Row.BucketDate).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestPersist_NonUUIDCluster(t *testing.T) {
	t.Parallel()
	err := persistRecommendations(context.Background(), commonFlags{output: "postgres://localhost/robne"}, recommendResult{
		OrgID:     "1234567",
		ClusterID: "local-cluster",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UUID")
}

func sampleResult() recommendResult {
	return recommendResult{
		OrgID:     "1234567",
		ClusterID: testutil.TestClusterUUID,
		Now:       time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC),
		Recs: []types.ContainerRec{{
			OrgID:            "1234567",
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        "app",
			Workload:         "api",
			WorkloadType:     "deployment",
			ContainerName:    "api",
			Term:             "short",
			Engine:           "cost",
			RecCPURequestMC:  58,
			RecCPULimitMC:    61,
			RecMemRequestKiB: 58880,
			RecMemLimitKiB:   61824,
		}},
	}
}

func sampleDigest() types.KeyedDigest {
	return types.KeyedDigest{
		Key: types.ContainerKey{
			Namespace:     "app",
			Workload:      "api",
			WorkloadType:  "deployment",
			ContainerName: "api",
		},
		Row: types.DigestRow{
			BucketDate:     time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			CPUUsageP95MC:  100,
			SampleCount:    24,
			MemUsageP95KiB: 1024,
		},
	}
}

func writeRobneYAML(t *testing.T, dir, orgID, clusterUUID string) {
	t.Helper()
	body := fmt.Sprintf("org_id: %q\ncluster_uuid: %q\n", orgID, clusterUUID)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "robne.yaml"), []byte(body), 0o600))
}

func dayCSV(ns, wl, cluster string, day time.Time) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,workload,workload_type,container_name,pod,cluster_id,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n")
	for h := 0; h < 24; h++ {
		start := time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, time.UTC)
		end := start.Add(time.Hour)
		fmt.Fprintf(&b, "%s,%s,%s,%s,deployment,%s,%s-0,%s,0.2,0.05,104857600,52428800\n",
			start.Format("2006-01-02 15:04:05 +0000 UTC"),
			end.Format("2006-01-02 15:04:05 +0000 UTC"),
			ns, wl, wl, wl, cluster)
	}
	return b.String()
}

func poolDSN(t *testing.T, pool *pgxpool.Pool, dbname string) string {
	t.Helper()
	c := pool.Config().ConnConfig
	if dbname == "" {
		dbname = c.Database
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))),
		Path:     "/" + dbname,
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

func createEmptyDatabase(t *testing.T, admin *pgxpool.Pool) string {
	t.Helper()
	name := "robne_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	return poolDSN(t, admin, name)
}
