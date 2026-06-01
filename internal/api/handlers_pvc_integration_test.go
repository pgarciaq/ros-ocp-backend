package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
