package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/reship"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestSettingsAPI_PUT_RecordsMasuRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	var captured struct {
		method string
		path   string
		query  string
	}
	masu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.query = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer masu.Close()

	t.Setenv("KOKU_MASU_URL", masu.URL)
	enableBusinessHoursForTest(t)

	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-masu-record"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, testutil.TestClusterUUID)

	trigger := reship.NewService(pool, reship.ServiceConfig{
		MasuURL: config.GetConfig().KokuMasuURL,
	})
	require.NotNil(t, trigger)
	e := setupBHTestEcho(t, pool, trigger, orgID)
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if captured.method == http.MethodPost {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	require.Equal(t, http.MethodPost, captured.method)
	require.Equal(t, "/api/cost-management/v1/reship_ros/", captured.path)
	require.Contains(t, captured.query, "schema=")
	require.Contains(t, captured.query, "provider_uuid="+testutil.TestClusterUUID)
	require.Contains(t, captured.query, "start_date=")
	require.Contains(t, captured.query, "end_date=")
}

func TestSettingsAPI_PUT_PostgresDown(t *testing.T) {
	enableBusinessHoursForTest(t)
	prev := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = prev })

	// Prevent initPool from connecting during test.
	t.Setenv("POSTGRES_SQL_SERVICE_HOST", "")
	config.ResetForTest()

	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-pg-down")
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-pg-down", validBHPayload())
	// Validation passes; pool acquisition fails when nil pool triggers init or returns unavailable.
	require.True(t, rec.Code == http.StatusServiceUnavailable || rec.Code >= 500,
		"expected 503/5xx when database unavailable, got %d body=%s", rec.Code, rec.Body.String())
}
