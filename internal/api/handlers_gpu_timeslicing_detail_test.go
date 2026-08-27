package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedTimeslicingDetailOrgCluster(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)
	var tenantID int
	err = pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, $3, $3, now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID, "ts-bh-"+t.Name())
	require.NoError(t, err)
}

func insertTimeslicingPersistRow(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, nodeName, gpuModel string) {
	t.Helper()
	ctx := context.Background()
	candidates := `[{"namespace":"ml-team","workload":"training-a","container":"gpu-worker","sm_active_avg":0.12,"classification":"underutilized"}]`
	_, err := pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendations (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, confidence_level,
			candidate_count, impacted_count,
			candidate_containers, impacted_containers,
			notification_codes, estimated_savings_cents, savings_per_gpu_cents
		) VALUES (
			$1, $2, $3, $4, 'medium',
			4, 0.7, 0.7, 1, 0,
			$5::jsonb, '[]'::jsonb,
			'{36}', 40000, 10000
		)`,
		orgID, clusterUUID, nodeName, gpuModel, candidates)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM node_gpu_timeslicing_recommendations WHERE org_id = $1 AND node_name = $2`,
			orgID, nodeName)
	})
}

func seedTimeslicingGPUStreams(t *testing.T, pool *pgxpool.Pool, clusterUUID, nodeName, gpuModel string, containers []struct{ ns, wl, cn string }) {
	t.Helper()
	start := testutil.RecentStart()
	for _, c := range containers {
		for i := 0; i < 7; i++ {
			row := testutil.GPUDigestRow{
				IntervalStart:       start.AddDate(0, 0, i),
				ClusterUUID:         clusterUUID,
				Namespace:           c.ns,
				Workload:            c.wl,
				WorkloadType:        "deployment",
				ContainerName:       c.cn,
				GPUModelName:        gpuModel,
				NodeName:            nodeName,
				FBUsageMinMiB:       500,
				FBUsageMaxMiB:       2000,
				FBUsageAvgMiB:       1200,
				TensorPipeActiveMin: 0.01,
				TensorPipeActiveMax: 0.10,
				TensorPipeActiveAvg: 0.05,
				DRAMActiveMin:       0.02,
				DRAMActiveMax:       0.08,
				DRAMActiveAvg:       0.05,
				SMActiveMin:         0.09,
				SMActiveMax:         0.17,
				SMActiveAvg:         0.12,
			}
			testutil.SeedGPUDigest(t, pool, row)
			row.ScheduleType = "business_hours"
			testutil.SeedGPUDigest(t, pool, row)
		}
	}
}

func TestGetNodeGPUTimeslicingDetail_UnknownNode404(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := testutil.TestOrgID
	seedTimeslicingDetailOrgCluster(t, pool, orgID, testutil.TestClusterUUID)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing/no-such-ts-node", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetNodeGPUTimeslicingDetail_HomogeneousEmitsCode81(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-tsbh-homo"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "ts-bh-homo-node"
	gpuModel := "NVIDIA T4"
	seedTimeslicingDetailOrgCluster(t, pool, orgID, clusterUUID)
	insertTimeslicingPersistRow(t, pool, orgID, clusterUUID, nodeName, gpuModel)
	seedTimeslicingGPUStreams(t, pool, clusterUUID, nodeName, gpuModel, []struct{ ns, wl, cn string }{
		{"homo-ml", "training-a", "gpu-worker-a"},
		{"homo-ml", "training-b", "gpu-worker-b"},
		{"homo-ml", "inference", "gpu-worker-c"},
	})
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing/"+nodeName, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var detail model.NodeGPUTimeslicingDetailResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	require.NotEmpty(t, detail.Data)
	bh := detail.Data[0].BusinessHours
	require.NotNil(t, bh)
	require.NotNil(t, bh.RecommendedReplicas)
	assert.GreaterOrEqual(t, *bh.RecommendedReplicas, 2)
	require.Contains(t, bh.Notifications, "81")
	assert.Equal(t, int16(81), bh.Notifications["81"].Code)
	assert.Empty(t, bh.Reason)
	assert.Greater(t, bh.SMActiveAvg, float32(0), "Peak hours radar needs BH SM")
	assert.Greater(t, bh.FBUsageMaxMiB, float32(0), "Peak hours radar needs BH FB")
	if bh.TotalFBMiB != nil {
		assert.Greater(t, *bh.TotalFBMiB, int64(0))
	}
	bhJSON, err := json.Marshal(bh)
	require.NoError(t, err)
	assert.NotContains(t, string(bhJSON), "savings")

	listReq := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?filter%5Bnode%5D="+nodeName, nil)
	listReq.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	listRec := httptest.NewRecorder()
	app.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code, listRec.Body.String())
	assert.NotContains(t, listRec.Body.String(), `"business_hours"`)
	assert.NotContains(t, listRec.Body.String(), "GPU_TS_BH_CLUSTER_WINDOW")
}

func TestGetNodeGPUTimeslicingDetail_HeterogeneousOmitsNestedObject(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-tsbh-hetero"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "ts-bh-hetero-node"
	gpuModel := "NVIDIA T4"
	seedTimeslicingDetailOrgCluster(t, pool, orgID, clusterUUID)
	insertTimeslicingPersistRow(t, pool, orgID, clusterUUID, nodeName, gpuModel)
	seedTimeslicingGPUStreams(t, pool, clusterUUID, nodeName, gpuModel, []struct{ ns, wl, cn string }{
		{"hetero-ml", "training-a", "gpu-worker-a"},
		{"hetero-batch", "night-job", "gpu-worker-b"},
	})
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}))
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "hetero-batch",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "00:00", EndTime: "06:00", Enabled: true,
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing/"+nodeName, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var detail model.NodeGPUTimeslicingDetailResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	require.NotEmpty(t, detail.Data)
	assert.Nil(t, detail.Data[0].BusinessHours)
	assert.NotContains(t, rec.Body.String(), `"business_hours"`)
}

func TestGetNodeGPUTimeslicingDetail_NamespaceOnlyOmitsNestedObject(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-tsbh-nsonly"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "ts-bh-nsonly-node"
	gpuModel := "NVIDIA T4"
	seedTimeslicingDetailOrgCluster(t, pool, orgID, clusterUUID)
	insertTimeslicingPersistRow(t, pool, orgID, clusterUUID, nodeName, gpuModel)
	seedTimeslicingGPUStreams(t, pool, clusterUUID, nodeName, gpuModel, []struct{ ns, wl, cn string }{
		{"nsonly-ml", "training-a", "gpu-worker-a"},
		{"nsonly-ml", "training-b", "gpu-worker-b"},
	})
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "nsonly-ml",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing/"+nodeName, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var detail model.NodeGPUTimeslicingDetailResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	require.NotEmpty(t, detail.Data)
	assert.Nil(t, detail.Data[0].BusinessHours)
}

func TestGetNodeGPUTimeslicingDetail_KillSwitchOmitsNestedObject(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-tsbh-kill"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "ts-bh-kill-node"
	gpuModel := "NVIDIA T4"
	seedTimeslicingDetailOrgCluster(t, pool, orgID, clusterUUID)
	insertTimeslicingPersistRow(t, pool, orgID, clusterUUID, nodeName, gpuModel)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing/"+nodeName, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var detail model.NodeGPUTimeslicingDetailResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	require.NotEmpty(t, detail.Data)
	assert.Nil(t, detail.Data[0].BusinessHours)
	assert.False(t, strings.Contains(rec.Body.String(), `"business_hours"`))
}
