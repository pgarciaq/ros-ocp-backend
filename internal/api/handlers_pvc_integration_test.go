package api_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedPVCRecCluster(t *testing.T, orgID string) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'pvc-test', 1, NOW()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID,
	)
	require.NoError(t, err)
}

func insertPVCRecommendationWithIdle(
	t *testing.T,
	orgID, namespace, pvcName, storageClass, recType, lastSeenPod, vmName string,
	usageRatio float64,
	idleSince *time.Time,
	idleDurationDays *int,
) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			storageclass, last_seen_pod, vm_name, recommendation_type, usage_ratio, capacity_bytes, usage_bytes_max,
			idle_since, idle_duration_days,
			notification_codes, data_days, updated_at
		) VALUES ($1, $2, $3, $4, 'medium', $5, $6, $7, $8, $9, 10737418240, 1073741824, $10, $11, '{}', 14, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term)
		DO UPDATE SET storageclass = EXCLUDED.storageclass,
			last_seen_pod = EXCLUDED.last_seen_pod,
			vm_name = EXCLUDED.vm_name,
			recommendation_type = EXCLUDED.recommendation_type,
			usage_ratio = EXCLUDED.usage_ratio,
			idle_since = EXCLUDED.idle_since,
			idle_duration_days = EXCLUDED.idle_duration_days`,
		orgID, testutil.TestClusterUUID, namespace, pvcName, storageClass, lastSeenPod, vmName, recType, usageRatio,
		idleSince, idleDurationDays,
	)
	require.NoError(t, err)
}

func insertPVCRecommendation(t *testing.T, orgID, namespace, pvcName, storageClass, recType, lastSeenPod, vmName string, usageRatio float64) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			storageclass, last_seen_pod, vm_name, recommendation_type, usage_ratio, capacity_bytes, usage_bytes_max,
			notification_codes, data_days, updated_at
		) VALUES ($1, $2, $3, $4, 'medium', $5, $6, $7, $8, $9, 10737418240, 1073741824, '{}', 14, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term)
		DO UPDATE SET storageclass = EXCLUDED.storageclass,
			last_seen_pod = EXCLUDED.last_seen_pod,
			vm_name = EXCLUDED.vm_name,
			recommendation_type = EXCLUDED.recommendation_type,
			usage_ratio = EXCLUDED.usage_ratio`,
		orgID, testutil.TestClusterUUID, namespace, pvcName, storageClass, lastSeenPod, vmName, recType, usageRatio,
	)
	require.NoError(t, err)
}

func TestGetPVCRecommendations_FilterCluster(t *testing.T) {
	const otherCluster = "22222222-2222-2222-2222-222222222222"
	orgID := "org-pvc-cluster-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'pvc-other', 2, NOW()) ON CONFLICT DO NOTHING`, otherCluster)
	require.NoError(t, err)

	insertPVCRecommendation(t, orgID, "apps", "pvc-a", "gp3-csi", "oversized", "", "", 0.05)

	_, err = pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			recommendation_type, usage_ratio, notification_codes, data_days, updated_at
		) VALUES ($1, $2, 'apps', 'pvc-b', 'medium', 'healthy', 0.5, '{}', 14, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term) DO NOTHING`,
		orgID, otherCluster,
	)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[cluster]="+testutil.TestClusterUUID+"&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, testutil.TestClusterUUID, resp.Data[0].ClusterUUID)
	assert.Equal(t, "pvc-a", resp.Data[0].PersistentVolumeClaim)
}

func TestGetPVCRecommendations_FilterProject(t *testing.T) {
	orgID := "org-pvc-project-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "target-ns", "pvc-target", "gp3-csi", "oversized", "", "", 0.05)
	insertPVCRecommendation(t, orgID, "other-ns", "pvc-other", "gp3-csi", "healthy", "", "", 0.5)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[project]=target-ns&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "target-ns", resp.Data[0].Namespace)
	assert.Equal(t, "pvc-target", resp.Data[0].PersistentVolumeClaim)
}

func TestGetPVCRecommendations_FilterRecommendationType(t *testing.T) {
	orgID := "org-pvc-type-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "apps", "pvc-oversized", "gp3-csi", "oversized", "", "", 0.05)
	insertPVCRecommendation(t, orgID, "apps", "pvc-healthy", "gp3-csi", "healthy", "", "", 0.5)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[recommendation_type]=oversized&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "oversized", resp.Data[0].RecommendationType)
	assert.Equal(t, "pvc-oversized", resp.Data[0].PersistentVolumeClaim)
}

func TestGetPVCRecommendations_FilterStorageClass(t *testing.T) {
	orgID := "org-pvc-sc-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "apps", "data-gp3", "gp3-csi", "", "", "oversized", 0.05)
	insertPVCRecommendation(t, orgID, "apps", "data-odf", "ocs-storagecluster-ceph-rbd", "", "", "near_full", 0.92)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[storageclass]=gp3-csi&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "gp3-csi", resp.Data[0].StorageClass)
	assert.Equal(t, "data-gp3", resp.Data[0].PersistentVolumeClaim)
}

func TestGetPVCRecommendations_OrderByEstimatedSavingsAsc(t *testing.T) {
	orgID := "org-pvc-order-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)

	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			recommendation_type, usage_ratio, estimated_savings_cents, updated_at
		) VALUES
			($1, $2, 'ns', 'pvc-low', 'medium', 'oversized', 0.1, 100, NOW()),
			($1, $2, 'ns', 'pvc-high', 'medium', 'oversized', 0.2, 50000, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term) DO NOTHING`,
		orgID, testutil.TestClusterUUID,
	)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?order_by=estimated_monthly_savings&order_how=asc&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.GreaterOrEqual(t, resp.Meta.Count, 2)
	require.GreaterOrEqual(t, len(resp.Data), 2)
	assert.Equal(t, "pvc-low", resp.Data[0].PersistentVolumeClaim)
	assert.Equal(t, "pvc-high", resp.Data[len(resp.Data)-1].PersistentVolumeClaim)
}

func TestGetPVCRecommendations_MountedByInResponse(t *testing.T) {
	orgID := "org-pvc-pod-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "kubevirt", "vm-disk", "ocs-storagecluster-ceph-rbd",
		"healthy", "virt-launcher-fedora-vm-x9y8z", "fedora-vm", 0.5)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/pvcs?limit=20", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "virt-launcher-fedora-vm-x9y8z", resp.Data[0].MountedBy)
	assert.Equal(t, "fedora-vm", resp.Data[0].VMName)
}

func TestGetPVCRecommendations_VMNameInResponse(t *testing.T) {
	orgID := "org-pvc-vmname-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "kubevirt", "datavol-fedora", "ocs-storagecluster-ceph-rbd",
		"healthy", "virt-launcher-fedora-vm-x9y8z", "fedora-vm", 0.4)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/pvcs?limit=20", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "fedora-vm", resp.Data[0].VMName)
}

func TestGetPVCRecommendationDetail_AllTermsAndHistory(t *testing.T) {
	orgID := "org-pvc-detail-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		DELETE FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'apps' AND persistentvolumeclaim = 'detail-pvc'`,
		orgID, testutil.TestClusterUUID,
	)
	require.NoError(t, err)

	for _, term := range []string{"short", "medium", "long"} {
		_, err := pool.Exec(ctx, `
			INSERT INTO pvc_recommendation_sets (
				org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
				recommendation_type, usage_ratio, capacity_bytes, usage_bytes_max,
				days_to_full, growth_bytes_per_day, notification_codes, data_days, updated_at
			) VALUES ($1, $2, 'apps', 'detail-pvc', $3, 'near_full', 0.9, 10737418240, 9663676416, 14, 104857600, '{1}', 30, NOW())`,
			orgID, testutil.TestClusterUUID, term,
		)
		require.NoError(t, err)
	}

	bucketDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := bucketDate.AddDate(0, 1, 0)
	partName := fmt.Sprintf("daily_pvc_digests_%s", bucketDate.Format("200601"))
	_, err = pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_pvc_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName, bucketDate.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO daily_pvc_digests (
			bucket_date, org_id, cluster_uuid, namespace, persistentvolumeclaim,
			capacity_bytes, usage_bytes_min, usage_bytes_max, usage_bytes_avg, sample_count
		) VALUES ($1::date, $2, $3, 'apps', 'detail-pvc', 10737418240, 5000000000, 9000000000, 7000000000, 24)
		ON CONFLICT (cluster_uuid, namespace, persistentvolumeclaim, bucket_date) DO NOTHING`,
		bucketDate.Format("2006-01-02"), orgID, testutil.TestClusterUUID,
	)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs/detail", api.GetPVCRecommendationDetail)

	url := "/api/cost-management/v1/recommendations/openshift/pvcs/detail" +
		"?cluster_uuid=" + testutil.TestClusterUUID +
		"&namespace=apps&persistentvolumeclaim=detail-pvc"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var detail api.PVCRecommendationDetailResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	assert.Equal(t, "detail-pvc", detail.PersistentVolumeClaim)
	assert.Len(t, detail.Terms, 3)
	assert.Contains(t, detail.Terms, "medium")
	assert.NotNil(t, detail.Terms["medium"].DaysToFull)
	assert.NotNil(t, detail.Terms["medium"].GrowthBytesPerDay)
	assert.NotEmpty(t, detail.Terms["medium"].Notifications)
	require.Len(t, detail.HistoricalUsage, 1)
	assert.Equal(t, "2026-05-01", detail.HistoricalUsage[0].Date)
	assert.Equal(t, int64(9000000000), detail.HistoricalUsage[0].UsageBytesMax)
}

func insertPVCRecommendationForCSV(
	t *testing.T,
	orgID, namespace, pvcName, storageClass, recType, lastSeenPod, vmName, pv string,
	usageRatio float64,
	recommendedBytes int64,
	daysToFull int,
	growthBytesPerDay int64,
	dataDays int,
) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			storageclass, last_seen_pod, vm_name, persistentvolume,
			recommendation_type, usage_ratio, capacity_bytes, usage_bytes_max,
			recommended_bytes, days_to_full, growth_bytes_per_day,
			notification_codes, data_days, updated_at
		) VALUES ($1, $2, $3, $4, 'medium', $5, $6, $7, $8, $9, $10,
			10737418240, 1073741824, $11, $12, $13, '{}', $14, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term)
		DO UPDATE SET storageclass = EXCLUDED.storageclass,
			last_seen_pod = EXCLUDED.last_seen_pod,
			vm_name = EXCLUDED.vm_name,
			persistentvolume = EXCLUDED.persistentvolume,
			recommendation_type = EXCLUDED.recommendation_type,
			usage_ratio = EXCLUDED.usage_ratio,
			recommended_bytes = EXCLUDED.recommended_bytes,
			days_to_full = EXCLUDED.days_to_full,
			growth_bytes_per_day = EXCLUDED.growth_bytes_per_day,
			data_days = EXCLUDED.data_days`,
		orgID, testutil.TestClusterUUID, namespace, pvcName, storageClass, lastSeenPod, vmName, pv,
		recType, usageRatio, recommendedBytes, daysToFull, growthBytesPerDay, dataDays,
	)
	require.NoError(t, err)
}

func TestGetPVCRecommendations_CSVExport(t *testing.T) {
	orgID := "org-pvc-csv-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendationForCSV(
		t, orgID, "apps", "csv-pvc", "gp3-csi", "oversized",
		"deploy/my-app", "my-vm", "pv-csv-001", 0.05,
		2147483648, 30, 1048576, 14,
	)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?format=csv&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")

	reader := csv.NewReader(strings.NewReader(rec.Body.String()))
	rows, err := reader.ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 2)

	expectedHeader := []string{
		"cluster_uuid", "namespace", "persistentvolumeclaim", "mounted_by", "vm_name",
		"persistentvolume", "storageclass", "recommendation_type", "usage_ratio",
		"capacity_bytes", "usage_bytes_max", "recommended_bytes", "days_to_full",
		"growth_bytes_per_day", "estimated_monthly_savings_value", "estimated_monthly_savings_units",
		"confidence_level", "idle_since", "idle_duration_days", "data_days", "term",
	}
	assert.Equal(t, expectedHeader, rows[0])

	headerIndex := make(map[string]int, len(rows[0]))
	for i, col := range rows[0] {
		headerIndex[col] = i
	}
	dataRow := rows[1]
	assert.Equal(t, testutil.TestClusterUUID, dataRow[headerIndex["cluster_uuid"]])
	assert.Equal(t, "apps", dataRow[headerIndex["namespace"]])
	assert.Equal(t, "csv-pvc", dataRow[headerIndex["persistentvolumeclaim"]])
	assert.Equal(t, "deploy/my-app", dataRow[headerIndex["mounted_by"]])
	assert.Equal(t, "my-vm", dataRow[headerIndex["vm_name"]])
	assert.Equal(t, "pv-csv-001", dataRow[headerIndex["persistentvolume"]])
	assert.Equal(t, "oversized", dataRow[headerIndex["recommendation_type"]])
	assert.Equal(t, "2147483648", dataRow[headerIndex["recommended_bytes"]])
	assert.Equal(t, "30", dataRow[headerIndex["days_to_full"]])
	assert.Equal(t, "1048576", dataRow[headerIndex["growth_bytes_per_day"]])
	assert.Equal(t, "14", dataRow[headerIndex["data_days"]])
	assert.Equal(t, "medium", dataRow[headerIndex["term"]])
	assert.Empty(t, dataRow[headerIndex["idle_since"]])
	assert.Empty(t, dataRow[headerIndex["idle_duration_days"]])
}

func TestGetPVCRecommendations_FilterTerm(t *testing.T) {
	orgID := "org-pvc-term-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			recommendation_type, usage_ratio, notification_codes, data_days, updated_at
		) VALUES ($1, $2, 'apps', 'pvc-medium', 'medium', 'oversized', 0.1, '{}', 14, NOW()),
			($1, $2, 'apps', 'pvc-short', 'short', 'healthy', 0.5, '{}', 7, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term) DO NOTHING`,
		orgID, testutil.TestClusterUUID,
	)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[term]=medium&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "medium", resp.Data[0].Term)
	assert.Equal(t, "pvc-medium", resp.Data[0].PersistentVolumeClaim)
}

func TestGetPVCRecommendations_FilterTermShortTermAlias(t *testing.T) {
	orgID := "org-pvc-term-alias-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			recommendation_type, usage_ratio, notification_codes, data_days, updated_at
		) VALUES ($1, $2, 'apps', 'pvc-short-alias', 'short', 'healthy', 0.5, '{}', 7, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term) DO NOTHING`,
		orgID, testutil.TestClusterUUID,
	)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[term]=short_term&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "short", resp.Data[0].Term)
	assert.Equal(t, "pvc-short-alias", resp.Data[0].PersistentVolumeClaim)
}

func TestGetPVCRecommendations_FilterTag(t *testing.T) {
	orgID := "org-pvc-tag-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "apps", "pvc-tagged", "gp3-csi", "healthy", "", "", 0.4)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[tag:environment]=production&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Meta.Count, 0)
}

func TestGetPVCRecommendations_OrphanedIdleFields(t *testing.T) {
	orgID := "org-pvc-idle-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	idleSince := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	idleDays := 14
	insertPVCRecommendationWithIdle(
		t, orgID, "archive", "idle-vol", "gp3-csi", "orphaned", "", "",
		0.0, &idleSince, &idleDays,
	)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[recommendation_type]=orphaned&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "orphaned", resp.Data[0].RecommendationType)
	require.NotNil(t, resp.Data[0].IdleSince)
	assert.Equal(t, "2026-05-01", *resp.Data[0].IdleSince)
	require.NotNil(t, resp.Data[0].IdleDurationDays)
	assert.Greater(t, *resp.Data[0].IdleDurationDays, 0)
}
