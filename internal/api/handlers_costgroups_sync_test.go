package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costgroups"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func costgroupsTestEcho(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	e := echo.New()
	e.POST("/internal/cost-groups/sync", api.PostCostGroupsSync)
	t.Cleanup(func() {
		database.Pool = nil
		_, _ = pool.Exec(context.Background(), `DELETE FROM hcp_platform_namespaces WHERE org_id = $1`, orgID)
	})
	return e
}

func TestPostCostGroupsSync_AuthAndValidation(t *testing.T) {
	orgID := "org-costgroups-api-" + t.Name()
	e := costgroupsTestEcho(t, orgID)

	body, err := json.Marshal(costgroups.SyncRequest{
		OrgID:    orgID,
		SyncedAt: "2026-10-07T12:00:00Z",
		Entries:  []costgroups.SyncEntry{{Namespace: "acme-team"}},
	})
	require.NoError(t, err)

	t.Run("no token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/cost-groups/sync", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		config.ResetTagsForTest()
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")
		req := httptest.NewRequest(http.MethodPost, "/internal/cost-groups/sync", bytes.NewReader([]byte("{not json")))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("happy path stores and re-POST converges", func(t *testing.T) {
		config.ResetTagsForTest()
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodPost, "/internal/cost-groups/sync", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
			var resp costgroups.SyncResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, 1, resp.Updated)
		}
	})
}
