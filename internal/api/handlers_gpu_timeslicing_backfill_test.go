package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestPostBackfillGPUTimeslicing_AuthAndProcessing(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")

	e := echo.New()
	e.POST("/internal/backfill-gpu-timeslicing", api.PostBackfillGPUTimeslicing)

	t.Run("missing token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost,
			"/internal/backfill-gpu-timeslicing?org_id="+testutil.TestOrgID, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("with dev token processes cluster", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost,
			"/internal/backfill-gpu-timeslicing?org_id=org-no-such-gpu-data", nil)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}
