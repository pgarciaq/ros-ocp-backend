package api_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestGetPVCRecommendations_CursorPagination(t *testing.T) {
	orgID := "org-pvc-cursor-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "apps", "pvc-a", "gp3-csi", "oversized", "", "", 0.05)
	insertPVCRecommendation(t, orgID, "apps", "pvc-b", "gp3-csi", "healthy", "", "", 0.50)
	insertPVCRecommendation(t, orgID, "data", "pvc-c", "gp3-csi", "near_full", "", "", 0.90)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	identityHeader := makeIdentityHeader(orgID)

	firstReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/pvcs?limit=1&order_by=usage_ratio&order_how=desc", nil)
	firstReq.Header.Set("X-Rh-Identity", identityHeader)
	firstRec := httptest.NewRecorder()
	app.ServeHTTP(firstRec, firstReq)
	require.Equal(t, http.StatusOK, firstRec.Code, firstRec.Body.String())

	var firstPage api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(firstRec.Body.Bytes(), &firstPage))
	require.Len(t, firstPage.Data, 1)
	assert.True(t, firstPage.Meta.HasNext)
	require.NotEmpty(t, firstPage.Meta.NextCursor)

	secondReq := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/cost-management/v1/recommendations/openshift/pvcs?limit=1&after=%s", firstPage.Meta.NextCursor),
		nil)
	secondReq.Header.Set("X-Rh-Identity", identityHeader)
	secondRec := httptest.NewRecorder()
	app.ServeHTTP(secondRec, secondReq)
	require.Equal(t, http.StatusOK, secondRec.Code, secondRec.Body.String())

	var secondPage api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(secondRec.Body.Bytes(), &secondPage))
	require.Len(t, secondPage.Data, 1)
	assert.NotEqual(t, firstPage.Data[0].PersistentVolumeClaim, secondPage.Data[0].PersistentVolumeClaim)
}

func TestGetPVCRecommendations_LowConfidenceBeforeMinDataDays(t *testing.T) {
	orgID := "org-pvc-conf-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendationWithIdle(t, orgID, "apps", "pvc-early", "gp3-csi", "oversized", "", "", 0.05, nil, nil)

	_, err := pool.Exec(context.Background(), `
		UPDATE pvc_recommendation_sets SET data_days = 2, recommendation_type = 'oversized'
		WHERE org_id = $1 AND persistentvolumeclaim = 'pvc-early'`, orgID)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/pvcs?limit=20", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.PVCRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	found := false
	for _, row := range resp.Data {
		if row.PersistentVolumeClaim == "pvc-early" {
			found = true
			assert.Equal(t, "oversized", row.RecommendationType)
			assert.InDelta(t, 2.0/14.0, float64(row.ConfidenceLevel), 0.05)
		}
	}
	assert.True(t, found)
}
