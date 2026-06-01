package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func insertPVCRecommendation(t *testing.T, orgID, namespace, pvcName, storageClass, recType string, usageRatio float64) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			storageclass, recommendation_type, usage_ratio, capacity_bytes, usage_bytes_max,
			notification_codes, data_days, updated_at
		) VALUES ($1, $2, $3, $4, 'medium', $5, $6, $7, 10737418240, 1073741824, '{}', 14, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term)
		DO UPDATE SET storageclass = EXCLUDED.storageclass,
			recommendation_type = EXCLUDED.recommendation_type,
			usage_ratio = EXCLUDED.usage_ratio`,
		orgID, testutil.TestClusterUUID, namespace, pvcName, storageClass, recType, usageRatio,
	)
	require.NoError(t, err)
}

func TestGetPVCRecommendations_FilterStorageClass(t *testing.T) {
	orgID := "org-pvc-sc-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "apps", "data-gp3", "gp3-csi", "oversized", 0.05)
	insertPVCRecommendation(t, orgID, "apps", "data-odf", "ocs-storagecluster-ceph-rbd", "near_full", 0.92)

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
			recommendation_type, usage_ratio, estimated_monthly_savings_usd, updated_at
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
