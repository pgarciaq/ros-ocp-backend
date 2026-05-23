package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/reship"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

type reshipCall struct {
	OrgID         string
	ClusterUUID   uuid.UUID
}

type recordingReshipTrigger struct { // implements reship.Triggerer
	mu          sync.Mutex
	calls       []reshipCall
	inFlight    int32
	maxInFlight int32
	delay       time.Duration
}

func (r *recordingReshipTrigger) TriggerReship(_ context.Context, orgID string, clusterUUID uuid.UUID) error {
	cur := atomic.AddInt32(&r.inFlight, 1)
	defer atomic.AddInt32(&r.inFlight, -1)
	for {
		max := atomic.LoadInt32(&r.maxInFlight)
		if cur <= max || atomic.CompareAndSwapInt32(&r.maxInFlight, max, cur) {
			break
		}
	}
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	r.mu.Lock()
	r.calls = append(r.calls, reshipCall{OrgID: orgID, ClusterUUID: clusterUUID})
	r.mu.Unlock()
	return nil
}

func (r *recordingReshipTrigger) snapshot() []reshipCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]reshipCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func enableBusinessHoursForTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()
}

func validBHPayload() map[string]interface{} {
	return map[string]interface{}{
		"timezone": "America/New_York",
		"schedule": map[string]interface{}{
			"days":       []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
			"start_time": "08:00",
			"end_time":   "17:00",
		},
		"off_hours_weight": 0.0,
		"enabled":          true,
	}
}

func setupBHTestEcho(t *testing.T, pool *pgxpool.Pool, trigger reship.Triggerer, orgID string) *echo.Echo {
	t.Helper()
	enableBusinessHoursForTest(t)
	if pool != nil {
		prev := db.Pool
		db.Pool = pool
		t.Cleanup(func() { db.Pool = prev })
	}

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: orgID},
			})
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	RegisterBusinessHoursRoutes(v1, NewBusinessHoursSettingsHandler(trigger))
	return e
}

func serveBH(t *testing.T, e *echo.Echo, method, path, orgID string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func seedBHCluster(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	var tenantID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO UPDATE SET org_id = EXCLUDED.org_id
		RETURNING id`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2::uuid, 'bh-test-cluster', 'src-bh', now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID)
	require.NoError(t, err)
}

func cleanupBHSchedules(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
}

func waitForReshipCalls(t *testing.T, trigger *recordingReshipTrigger, min int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(trigger.snapshot()) >= min {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected at least %d reship calls, got %d", min, len(trigger.snapshot()))
}

func TestSettingsAPI_GET_OrgDefault_NoRow(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-get-empty"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	rec := serveBH(t, e, http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, false, resp["enabled"])
	require.NotContains(t, resp, "timezone")
	require.NotContains(t, resp, "schedule")
}

func TestSettingsAPI_PUT_OrgDefault_ValidPayload(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-put-org"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, testutil.TestClusterUUID)

	trigger := &recordingReshipTrigger{}
	e := setupBHTestEcho(t, pool, trigger, orgID)
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, validBHPayload())

	require.Equal(t, http.StatusAccepted, rec.Code)
	waitForReshipCalls(t, trigger, 1)
	calls := trigger.snapshot()
	require.Equal(t, orgID, calls[0].OrgID)
	require.Equal(t, testutil.TestClusterUUID, calls[0].ClusterUUID.String())
}

func TestSettingsAPI_PUT_InvalidTimezone(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-validation")
	payload := validBHPayload()
	payload["timezone"] = "Not/A_Zone"
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-validation", payload)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_PUT_EmptyDays(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-validation")
	payload := validBHPayload()
	payload["schedule"] = map[string]interface{}{
		"days":       []string{},
		"start_time": "08:00",
		"end_time":   "17:00",
	}
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-validation", payload)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_PUT_InvalidDayName(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-validation")
	payload := validBHPayload()
	payload["schedule"] = map[string]interface{}{
		"days":       []string{"funday"},
		"start_time": "08:00",
		"end_time":   "17:00",
	}
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-validation", payload)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_PUT_InvalidTimeFormat(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-validation")
	payload := validBHPayload()
	payload["schedule"] = map[string]interface{}{
		"days":       []string{"monday"},
		"start_time": "8:00",
		"end_time":   "17:00",
	}
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-validation", payload)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_PUT_OvernightRejected(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-validation")
	payload := validBHPayload()
	payload["schedule"] = map[string]interface{}{
		"days":       []string{"monday"},
		"start_time": "22:00",
		"end_time":   "06:00",
	}
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-validation", payload)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_PUT_OffHoursWeightOutOfRange(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-validation")
	payload := validBHPayload()
	payload["off_hours_weight"] = 1.5
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-validation", payload)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_PUT_OffHoursWeightDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-weight-default"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, testutil.TestClusterUUID)

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	payload := validBHPayload()
	delete(payload, "off_hours_weight")
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, payload)
	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp businessHoursPutResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.InDelta(t, 0.0, resp.OffHoursWeight, 0.001)
}

func TestSettingsAPI_GET_Cluster_InheritsOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-inherit-cluster"
	cluster := testutil.TestClusterUUID
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, cluster)

	ctx := context.Background()
	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: engine.OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00",
		Enabled: true,
	}))

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	path := "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/" + cluster
	rec := serveBH(t, e, http.MethodGet, path, orgID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp businessHoursSettingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "America/New_York", resp.Timezone)
	assert.True(t, resp.Enabled)
}

func TestSettingsAPI_PUT_ClusterOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-cluster-put"
	cluster := testutil.TestClusterUUID
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, cluster)

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	payload := validBHPayload()
	payload["timezone"] = "America/Chicago"
	path := "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/" + cluster
	rec := serveBH(t, e, http.MethodPut, path, orgID, payload)
	require.Equal(t, http.StatusAccepted, rec.Code)

	ctx := context.Background()
	cache, err := engine.LoadSchedules(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	assert.Equal(t, "America/Chicago", cache.Resolve("any-ns").Timezone)
}

func TestSettingsAPI_GET_Namespace_InheritsChain(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-ns-chain"
	cluster := testutil.TestClusterUUID
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, cluster)
	ctx := context.Background()

	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: engine.OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00", Enabled: true,
	}))
	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: cluster, Namespace: "",
		Timezone: "America/Chicago", Days: []string{"tuesday"}, StartTime: "09:00", EndTime: "18:00", Enabled: true,
	}))

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	path := "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/" + cluster + "/namespaces/team-a"
	rec := serveBH(t, e, http.MethodGet, path, orgID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp businessHoursSettingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "America/Chicago", resp.Timezone)
}

func TestSettingsAPI_PUT_NamespaceEnabledFalse(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-ns-disabled"
	cluster := testutil.TestClusterUUID
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, cluster)
	ctx := context.Background()
	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: engine.OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00", Enabled: true,
	}))

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	payload := validBHPayload()
	payload["enabled"] = false
	path := "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/" + cluster + "/namespaces/team-a"
	rec := serveBH(t, e, http.MethodPut, path, orgID, payload)
	require.Equal(t, http.StatusAccepted, rec.Code)

	cache, err := engine.LoadSchedules(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	assert.False(t, cache.Resolve("team-a").Enabled)
	assert.True(t, cache.Resolve("other-ns").Enabled)
}

func TestSettingsAPI_DELETE_OrgDefault_Exists(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-del-org"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, testutil.TestClusterUUID)
	ctx := context.Background()
	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: engine.OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00", Enabled: true,
	}))

	trigger := &recordingReshipTrigger{}
	e := setupBHTestEcho(t, pool, trigger, orgID)
	rec := serveBH(t, e, http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code)
	waitForReshipCalls(t, trigger, 1)
}

func TestSettingsAPI_DELETE_OrgDefault_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-del-missing"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	rec := serveBH(t, e, http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSettingsAPI_PUTEnabledFalse_Vs_DELETE(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-false-vs-del"
	cluster := testutil.TestClusterUUID
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, cluster)
	ctx := context.Background()

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	nsPath := "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/" + cluster + "/namespaces/team-x"
	payload := validBHPayload()
	payload["enabled"] = false
	rec := serveBH(t, e, http.MethodPut, nsPath, orgID, payload)
	require.Equal(t, http.StatusAccepted, rec.Code)

	var count int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM business_hours_schedules
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = 'team-x'`, orgID, cluster).Scan(&count))
	require.Equal(t, 1, count)

	rec = serveBH(t, e, http.MethodDelete, nsPath, orgID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM business_hours_schedules
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = 'team-x'`, orgID, cluster).Scan(&count))
	require.Equal(t, 0, count)
}

func TestSettingsAPI_MissingIdentity(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	RegisterBusinessHoursRoutes(v1, NewBusinessHoursSettingsHandler(&recordingReshipTrigger{}))

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSettingsAPI_InvalidClusterID(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-bad-cluster"
	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)

	rec := serveBH(t, e, http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/not-a-uuid", orgID, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	unknown := "550e8400-e29b-41d4-a716-446655440099"
	rec = serveBH(t, e, http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/"+unknown, orgID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSettingsAPI_PUT_Returns202Not200(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-202"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, testutil.TestClusterUUID)

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)
}

func TestSettingsAPI_PUT_ClusterMarksReshipPending(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-pending-put"
	clusterID := testutil.TestClusterUUID
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, clusterID)

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	path := "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/" + clusterID
	rec := serveBH(t, e, http.MethodPut, path, orgID, validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)

	pending, err := reship.ReshipPendingSince(context.Background(), pool, orgID, uuid.MustParse(clusterID))
	require.NoError(t, err)
	require.NotNil(t, pending, "PUT must set reship_pending_since before async reship completes")
}

func TestSettingsAPI_GET_OrgDefault_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-roundtrip"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, testutil.TestClusterUUID)

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	payload := validBHPayload()
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, payload)
	require.Equal(t, http.StatusAccepted, rec.Code)

	rec = serveBH(t, e, http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp businessHoursSettingsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "America/New_York", resp.Timezone)
	assert.True(t, resp.Enabled)
	require.NotNil(t, resp.Schedule)
	assert.Equal(t, []string{"monday", "tuesday", "wednesday", "thursday", "friday"}, resp.Schedule.Days)
}

func TestSettingsAPI_PUT_ResponseIncludesStorageWarning(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-warning"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, testutil.TestClusterUUID)

	e := setupBHTestEcho(t, pool, &recordingReshipTrigger{}, orgID)
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp businessHoursPutResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Warnings, 1)
	assert.Contains(t, resp.Warnings[0], "doubles digest storage")
}

func TestSettingsAPI_PUT_DayNameCaseSensitive(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-validation")
	payload := validBHPayload()
	payload["schedule"] = map[string]interface{}{
		"days": []string{"Monday"}, "start_time": "08:00", "end_time": "17:00",
	}
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-validation", payload)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_PUT_EqualStartEnd(t *testing.T) {
	enableBusinessHoursForTest(t)
	e := setupBHTestEcho(t, nil, &recordingReshipTrigger{}, "org-bh-validation")
	payload := validBHPayload()
	payload["schedule"] = map[string]interface{}{
		"days": []string{"monday"}, "start_time": "08:00", "end_time": "08:00",
	}
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", "org-bh-validation", payload)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSettingsAPI_DELETE_AsyncReshipLikePUT(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-async-del"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, testutil.TestClusterUUID)
	ctx := context.Background()
	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID: orgID, ClusterUUID: engine.OrgClusterSentinelUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday"}, StartTime: "08:00", EndTime: "17:00", Enabled: true,
	}))

	trigger := &recordingReshipTrigger{delay: 200 * time.Millisecond}
	e := setupBHTestEcho(t, pool, trigger, orgID)
	start := time.Now()
	rec := serveBH(t, e, http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, nil)
	elapsed := time.Since(start)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Less(t, elapsed, 100*time.Millisecond, "DELETE should return before async reship completes")
	waitForReshipCalls(t, trigger, 1)
}

func TestSettingsAPI_Reship_OrgLevel_AllClusters(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-fanout"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })

	clusters := []string{
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3",
	}
	for _, c := range clusters {
		seedBHCluster(t, pool, orgID, c)
	}

	trigger := &recordingReshipTrigger{}
	e := setupBHTestEcho(t, pool, trigger, orgID)
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)
	waitForReshipCalls(t, trigger, len(clusters))
	assert.Len(t, trigger.snapshot(), len(clusters))
}

func TestSettingsAPI_Reship_OrgFanOut_MaxTwoConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-concurrency"
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })

	fiveClusters := []string{
		"bbbbbbbb-bbbb-bbbb-bbbb-000000000001",
		"bbbbbbbb-bbbb-bbbb-bbbb-000000000002",
		"bbbbbbbb-bbbb-bbbb-bbbb-000000000003",
		"bbbbbbbb-bbbb-bbbb-bbbb-000000000004",
		"bbbbbbbb-bbbb-bbbb-bbbb-000000000005",
	}
	for _, c := range fiveClusters {
		seedBHCluster(t, pool, orgID, c)
	}

	trigger := &recordingReshipTrigger{delay: 80 * time.Millisecond}
	e := setupBHTestEcho(t, pool, trigger, orgID)
	rec := serveBH(t, e, http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/business-hours", orgID, validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)
	waitForReshipCalls(t, trigger, 5)
	require.LessOrEqual(t, atomic.LoadInt32(&trigger.maxInFlight), int32(2))
}

func TestSettingsAPI_Reship_ScopedToClusterProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := "org-bh-scoped"
	cluster := testutil.TestClusterUUID
	cleanupBHSchedules(t, pool, orgID)
	t.Cleanup(func() { cleanupBHSchedules(t, pool, orgID) })
	seedBHCluster(t, pool, orgID, cluster)

	trigger := &recordingReshipTrigger{}
	e := setupBHTestEcho(t, pool, trigger, orgID)
	path := "/api/cost-management/v1/recommendations/openshift/settings/business-hours/clusters/" + cluster
	rec := serveBH(t, e, http.MethodPut, path, orgID, validBHPayload())
	require.Equal(t, http.StatusAccepted, rec.Code)
	waitForReshipCalls(t, trigger, 1)
	require.Equal(t, cluster, trigger.snapshot()[0].ClusterUUID.String())
}
