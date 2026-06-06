package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestSavingsSummary_DisplaysNegativeCorrectly lives in api_test (not engine) because the
// savings-summary handler lives in the api package and importing api from engine tests
// creates an import cycle.
func TestSavingsSummary_DisplaysNegativeCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-negative-savings-api"

	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (9002, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (9002, $1, 'neg-api-cluster', 'src-neg-api', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'worker-scale-up', 'medium', 'cost', '{}', -12345, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	e := echo.New()
	e.Use(contractTestIdentityMiddleware(orgID))
	v1 := e.Group(apiV1Prefix)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)

	req := httptest.NewRequest(http.MethodGet, apiV1Prefix+"/recommendations/openshift/savings-summary", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))

	savings, ok := raw["estimated_monthly_savings"].(map[string]interface{})
	require.True(t, ok, "estimated_monthly_savings should be present")
	value, ok := savings["value"].(string)
	require.True(t, ok)
	assert.Equal(t, "-123.45", value, "total should not be clamped to zero")
	assert.Equal(t, "USD", savings["units"])

	byPlugin, ok := raw["by_plugin"].(map[string]interface{})
	require.True(t, ok)
	nodeSavings, ok := byPlugin["node"].(map[string]interface{})
	require.True(t, ok, "by_plugin.node should be a MoneyAmount object")
	nodeValue, ok := nodeSavings["value"].(string)
	require.True(t, ok)
	assert.Equal(t, "-123.45", nodeValue, "by_plugin.node should preserve negative value for UI")
	assert.Equal(t, "USD", nodeSavings["units"])
}
