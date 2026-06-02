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
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestPostRecalculateSavings_AcceptsQuotaTypes(t *testing.T) {
	testutil.SetupTestDB(t)

	e := echo.New()
	e.POST("/internal/recalculate-savings", api.PostRecalculateSavings)

	t.Run("disabled returns 404", func(t *testing.T) {
		config.ResetForTest()
		t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "false")

		body, err := json.Marshal(map[string]any{
			"org_id":               testutil.TestOrgID,
			"recommendation_types": []string{"quota", "cluster-quota"},
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/internal/recalculate-savings", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("invalid types returns 400", func(t *testing.T) {
		config.ResetForTest()
		t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
		t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")
		tags.ResetProviderForTest()

		body, err := json.Marshal(map[string]any{
			"org_id":               testutil.TestOrgID,
			"recommendation_types": []string{"vm"},
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/internal/recalculate-savings", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("quota types accepted", func(t *testing.T) {
		config.ResetForTest()
		t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
		t.Setenv("ROS_SAVINGS_RECALCULATION_ENABLED", "true")
		t.Setenv("ROS_TAGS_DEV_TOKEN", "dev-token")
		tags.ResetProviderForTest()

		triggered := false
		engine.SetSavingsRecalcHookForTest(func(orgID string, recTypes []string) {
			triggered = true
			assert.Equal(t, testutil.TestOrgID, orgID)
			assert.Equal(t, []string{"quota", "cluster-quota"}, recTypes)
		})
		t.Cleanup(engine.ClearSavingsRecalcHookForTest)

		body, err := json.Marshal(map[string]any{
			"org_id":               testutil.TestOrgID,
			"recommendation_types": []string{"quota", "cluster-quota"},
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/internal/recalculate-savings", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set(echo.HeaderAuthorization, "Bearer dev-token")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code, "body: %s", rec.Body.String())

		var resp api.SavingsRecalcResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "accepted", resp.Status)
		assert.Equal(t, []string{"quota", "cluster-quota"}, resp.RecommendationTypes)
		assert.True(t, triggered)
	})
}
