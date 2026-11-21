package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/container"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/gpu"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/namespace"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/node"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/pvc"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

type termsResponse struct {
	RecommendationType string     `json:"recommendation_type"`
	Terms              []termItem `json:"terms"`
}

type termItem struct {
	Name               string  `json:"name"`
	WindowDays         int     `json:"window_days"`
	MinDataDays        int     `json:"min_data_days"`
	DecayHalfLifeHours float64 `json:"decay_halflife_hours"`
	Locked             bool    `json:"locked"`
	IsDefault          bool    `json:"is_default"`
}

func setupTermsApp(t *testing.T) (*echo.Echo, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/settings/terms", api.GetTermSettings)
	v1.PUT("/recommendations/openshift/settings/terms", api.PutTermSettings)
	v1.DELETE("/recommendations/openshift/settings/terms", api.DeleteTermSettings)
	v1.GET("/recommendations/openshift/settings/capabilities", api.GetCapabilities)

	return app, makeIdentityHeader(testutil.TestOrgID)
}

func TestGetTermSettings_ReturnsDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "container", resp.RecommendationType)
	require.Len(t, resp.Terms, 3)

	assert.Equal(t, "short", resp.Terms[0].Name)
	assert.Equal(t, 1, resp.Terms[0].WindowDays)
	assert.Equal(t, 1, resp.Terms[0].MinDataDays)
	assert.True(t, resp.Terms[0].IsDefault)
	assert.False(t, resp.Terms[0].Locked)

	assert.Equal(t, "medium", resp.Terms[1].Name)
	assert.Equal(t, 7, resp.Terms[1].WindowDays)

	assert.Equal(t, "long", resp.Terms[2].Name)
	assert.Equal(t, 15, resp.Terms[2].WindowDays)
}

// TestGetTermSettings_InvalidEnvValueIsNotLocked pins #792: an invalid admin
// env value must never be reported as locked — `locked: true` must not
// describe a value that was never applied. A locked-but-unapplied term is
// the worst of both worlds: the admin's intent is dropped AND the tenant is
// forbidden from correcting it. (In production the value never gets this
// far — startup refuses it — but the invariant must hold structurally, not
// by startup accident.)
func TestGetTermSettings_InvalidEnvValueIsNotLocked(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)
	// The term config cache (60s TTL) is shared across tests via the test
	// org — reset this org+type entry so the env var under test is the
	// only influence on the resolution.
	engine.InvalidateTermCache(testutil.TestOrgID, "container")

	// 120 exceeds the container plugin's 90-day window cap.
	t.Setenv("ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS", "120")

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)
	long := resp.Terms[2]
	require.Equal(t, "long", long.Name)

	assert.False(t, long.Locked,
		"a term whose env value was rejected must not be reported as locked — the tenant must be able to set it (#792)")
	assert.Equal(t, 15, long.WindowDays,
		"the rejected env value must not be applied; the plugin default holds")
	assert.True(t, long.IsDefault, "the term must present as the default (nothing overridden)")
}

// TestGetTermSettings_ValidEnvLocksAndApplies pins the other half of #792's
// invariant: locked implies applied. A valid admin env value locks the term
// AND takes effect — locked and the effective value come from the same
// resolution step.
func TestGetTermSettings_ValidEnvLocksAndApplies(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)
	engine.InvalidateTermCache(testutil.TestOrgID, "container")

	t.Setenv("ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS", "45")

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)
	long := resp.Terms[2]
	require.Equal(t, "long", long.Name)

	assert.True(t, long.Locked, "a valid admin env value locks the term")
	assert.Equal(t, 45, long.WindowDays,
		"locked implies applied — the env value must take effect (#792 invariant)")
	assert.False(t, long.IsDefault)
}

func TestGetTermSettings_RequiresRecommendationType(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetTermSettings_PVCDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=pvc", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "pvc", resp.RecommendationType)
	require.Len(t, resp.Terms, 3)

	assert.Equal(t, "short", resp.Terms[0].Name)
	assert.Equal(t, 7, resp.Terms[0].WindowDays)
	assert.Equal(t, 3, resp.Terms[0].MinDataDays)

	assert.Equal(t, "medium", resp.Terms[1].Name)
	assert.Equal(t, 30, resp.Terms[1].WindowDays)
	assert.Equal(t, 14, resp.Terms[1].MinDataDays)

	assert.Equal(t, "long", resp.Terms[2].Name)
	assert.Equal(t, 90, resp.Terms[2].WindowDays)
	assert.Equal(t, 30, resp.Terms[2].MinDataDays)
}

func TestPutTermSettings_CustomTerms(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	body := `{"terms":[
		{"name":"short","window_days":3,"decay_halflife_hours":0},
		{"name":"medium","window_days":14,"decay_halflife_hours":200},
		{"name":"long","window_days":30,"decay_halflife_hours":400}
	]}`

	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container",
		strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)

	assert.Equal(t, "short", resp.Terms[0].Name)
	assert.Equal(t, 3, resp.Terms[0].WindowDays)
	assert.False(t, resp.Terms[0].IsDefault)

	assert.Equal(t, "medium", resp.Terms[1].Name)
	assert.Equal(t, 14, resp.Terms[1].WindowDays)

	assert.Equal(t, "long", resp.Terms[2].Name)
	assert.Equal(t, 30, resp.Terms[2].WindowDays)
	assert.Equal(t, 15, resp.Terms[2].MinDataDays)

	// GET should now return custom terms.
	req2 := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	req2.Header.Set("X-Rh-Identity", identity)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp2 termsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, 3, resp2.Terms[0].WindowDays)
	assert.False(t, resp2.Terms[0].IsDefault)
}

func TestDeleteTermSettings_RestoresDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	// Set custom terms.
	body := `{"terms":[{"name":"short","window_days":5},{"name":"medium","window_days":20}]}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container",
		strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// DELETE to restore defaults.
	req2 := httptest.NewRequest(http.MethodDelete,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	req2.Header.Set("X-Rh-Identity", identity)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)
	assert.Equal(t, 1, resp.Terms[0].WindowDays)
	assert.True(t, resp.Terms[0].IsDefault)
}

func TestPutTermSettings_ValidationErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	tests := []struct {
		name string
		body string
	}{
		{"empty terms", `{"terms":[]}`},
		{"window_days too low", `{"terms":[{"name":"short","window_days":0}]}`},
		{"window_days too high", `{"terms":[{"name":"short","window_days":400}]}`},
		{"invalid name", `{"terms":[{"name":"daily","window_days":1}]}`},
		{"negative decay", `{"terms":[{"name":"short","window_days":1,"decay_halflife_hours":-1}]}`},
		{"decay exceeds max", `{"terms":[{"name":"medium","window_days":14,"decay_halflife_hours":9000}]}`},
		{"too many terms", `{"terms":[{"name":"short","window_days":1},{"name":"medium","window_days":7},{"name":"long","window_days":15},{"name":"short","window_days":2}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut,
				"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container",
				strings.NewReader(tt.body))
			req.Header.Set("X-Rh-Identity", identity)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "case %q: body: %s", tt.name, rec.Body.String())
		})
	}
}

func TestPutTermSettings_LockedTermReturns422(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	// Lock the "long" term via env var.
	t.Setenv("ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS", "45")

	body := `{"terms":[{"name":"long","window_days":30}]}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container",
		strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
}

func TestPutTermSettings_OrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/settings/terms", api.GetTermSettings)
	v1.PUT("/recommendations/openshift/settings/terms", api.PutTermSettings)

	org1Header := makeIdentityHeader("org-alpha")
	org2Header := makeIdentityHeader("org-beta")

	// org1 sets custom terms.
	body := `{"terms":[{"name":"short","window_days":10}]}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container",
		strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", org1Header)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// org2 should still see defaults.
	req2 := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	req2.Header.Set("X-Rh-Identity", org2Header)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3, "org2 should see all 3 default terms")
	assert.Equal(t, 1, resp.Terms[0].WindowDays, "org2 short term should be default 1d")
	assert.True(t, resp.Terms[0].IsDefault)
}

func TestGetCapabilities(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/capabilities", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		RecommendationTypes []struct {
			Name          string `json:"name"`
			SupportsTerms bool   `json:"supports_terms"`
			Enabled       bool   `json:"enabled"`
		} `json:"recommendation_types"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Check that known term-supporting plugins are listed correctly.
	termPlugins := map[string]bool{}
	for _, rt := range resp.RecommendationTypes {
		if rt.SupportsTerms {
			termPlugins[rt.Name] = true
		}
	}
	assert.True(t, termPlugins["container"], "container plugin should support terms")
	assert.True(t, termPlugins["pvc"], "pvc plugin should support terms")
	assert.True(t, termPlugins["gpu"], "gpu plugin should support terms")
	assert.True(t, termPlugins["node"], "node plugin should support terms")
	assert.True(t, termPlugins["namespace"], "namespace plugin should support terms")
}

// TestGetTermSettings_HCPUnsupported is #793's RED: the terms settings
// endpoint must reject recommendation_type=hcp — the HCP plugin is
// term-less by design (ADR-0346: the fleet-capacity advisory is a
// snapshot-series model; #630's design line: "supports_terms:false v1
// reusing container windows"). The scaffold's stray TermProvider made
// the trait walk accept hcp — a knob storing values no computation
// reads (the HCP detail rides the container terms; the advisories use
// correlator policy windows). Baseline: the endpoint returns 200 and
// echoes the scaffolded defaults.
func TestGetTermSettings_HCPUnsupported(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=hcp", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"hcp is term-less by design (ADR-0346) — the terms endpoint must reject it, not offer an inert knob (#793); body: %s", rec.Body.String())
}

// Control: the other term-less plugins already reject — quota is the
// existing behavior the hcp fix aligns with.
func TestGetTermSettings_QuotaUnsupported(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=quota", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"quota is term-less (threshold-based) — the existing term-less rejection (#793 control)")
}

// getVMTerms fetches the generic terms route for VM with a cache-busted
// resolution, so the env state at call time is the only influence.
func getVMTerms(t *testing.T, app *echo.Echo, identity string) termsResponse {
	t.Helper()
	engine.InvalidateTermCache(testutil.TestOrgID, "vm")
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=vm", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)
	return resp
}

// TestGetTermSettings_VMNameStabilityAndLockAgreement (#866): the generic
// route serves VM in the plugin's own display vocabulary (short_term…),
// stable regardless of which override source filled the values, and its
// lock probes use the resolver's ord vocabulary — the spelling the env
// vars actually resolve. Pre-fix failures: the dead spelling reported
// locked (locked-but-unapplied), and any env/DB override flipped the
// served name to the ord spelling.
func TestGetTermSettings_VMNameStabilityAndLockAgreement(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)

	t.Run("env unset: display names, unlocked", func(t *testing.T) {
		resp := getVMTerms(t, app, identity)
		assert.Equal(t, "short_term", resp.Terms[0].Name)
		assert.Equal(t, "medium_term", resp.Terms[1].Name)
		assert.Equal(t, "long_term", resp.Terms[2].Name)
		assert.False(t, resp.Terms[0].Locked)
		assert.Equal(t, 7, resp.Terms[0].WindowDays)
	})

	t.Run("operative env: applied, locked, name stable", func(t *testing.T) {
		t.Setenv("ROS_TERMS_VM_SHORT_WINDOW_DAYS", "5")
		resp := getVMTerms(t, app, identity)
		assert.Equal(t, "short_term", resp.Terms[0].Name,
			"the served name must not flip to the ord spelling when an env override exists (#866)")
		assert.Equal(t, 5, resp.Terms[0].WindowDays, "the operative env value applies")
		assert.True(t, resp.Terms[0].Locked, "the operative spelling locks")
	})

	t.Run("dead spelling: not locked, default holds", func(t *testing.T) {
		// Bypasses startup validation inside the test; the route must not
		// probe a spelling the resolver never resolves.
		t.Setenv("ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS", "5")
		resp := getVMTerms(t, app, identity)
		assert.False(t, resp.Terms[0].Locked,
			"a spelling the resolver never resolves must not report locked — the locked-but-unapplied shape (#866)")
		assert.Equal(t, 7, resp.Terms[0].WindowDays, "the dead spelling applies nothing; the default holds")
	})

	t.Run("db override: name stable", func(t *testing.T) {
		_, err := database.Pool.Exec(context.Background(),
			`INSERT INTO org_recommendation_terms (org_id, recommendation_type, term_ord, window_days, min_data_days, decay_halflife_hours)
			 VALUES ($1, 'vm', 1, 9, 3, NULL) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = database.Pool.Exec(context.Background(),
				`DELETE FROM org_recommendation_terms WHERE org_id = $1 AND recommendation_type = 'vm'`, testutil.TestOrgID)
		})
		resp := getVMTerms(t, app, identity)
		assert.Equal(t, "short_term", resp.Terms[0].Name,
			"a tenant DB override must not flip the served name to the ord spelling (#866)")
		assert.Equal(t, 9, resp.Terms[0].WindowDays)
	})
}

// TestPutTermSettings_VMAcceptsDisplayNames (#866): the generic PUT must
// accept the names its own GET serves — the display vocabulary — not only
// the ord spellings.
func TestPutTermSettings_VMAcceptsDisplayNames(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	app, identity := setupTermsApp(t)
	engine.InvalidateTermCache(testutil.TestOrgID, "vm")
	t.Cleanup(func() {
		_, _ = database.Pool.Exec(context.Background(),
			`DELETE FROM org_recommendation_terms WHERE org_id = $1 AND recommendation_type = 'vm'`, testutil.TestOrgID)
	})

	body := `{"terms":[{"name":"short_term","window_days":9,"min_data_days":3}]}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=vm",
		strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String(),
		"the generic PUT must accept the names its own GET serves (#866)")

	// Round-trip: the GET serves the display name with the stored value.
	resp := getVMTerms(t, app, identity)
	assert.Equal(t, "short_term", resp.Terms[0].Name)
	assert.Equal(t, 9, resp.Terms[0].WindowDays)
	assert.False(t, resp.Terms[0].IsDefault)
}
