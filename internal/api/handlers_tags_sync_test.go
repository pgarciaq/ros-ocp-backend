package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestPostTagsSync_FeatureGateAndAuth(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	e := echo.New()
	e.POST("/internal/tags/sync", api.PostTagsSync)

	body, err := json.Marshal(tags.SyncRequest{
		OrgID: testutil.TestOrgID,
		ContainerTags: []tags.ContainerTags{
			{
				ClusterUUID:   testutil.TestClusterUUID,
				Namespace:     "ns",
				Workload:      "wl",
				ContainerName: "ctr",
				Tags:          map[string]string{"environment": "prod"},
			},
		},
	})
	require.NoError(t, err)

	t.Run("disabled returns 404", func(t *testing.T) {
		config.ResetTagsForTest()
		t.Setenv("ROS_TAGS_ENABLED", "false")

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("enabled without token returns 401", func(t *testing.T) {
		config.ResetTagsForTest()
		t.Setenv("ROS_TAGS_ENABLED", "true")
		t.Setenv("ROS_TAGS_INTERNAL_TOKEN", "secret-token")

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("enabled with token updates rows", func(t *testing.T) {
		config.ResetTagsForTest()
		t.Setenv("ROS_TAGS_ENABLED", "true")
		t.Setenv("ROS_TAGS_INTERNAL_TOKEN", "secret-token")

		_, err := pool.Exec(t.Context(), `
			INSERT INTO org_container_keys (
				org_id, cluster_uuid, namespace, workload, workload_type, container_name
			) VALUES ($1, $2, 'ns', 'wl', 'Deployment', 'ctr')
			ON CONFLICT DO NOTHING`,
			testutil.TestOrgID, testutil.TestClusterUUID,
		)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/internal/tags/sync", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set("X-ROS-Internal-Token", "secret-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp tags.SyncResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Updated)
	})
}
