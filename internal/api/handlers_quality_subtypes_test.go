package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// Subtype quality endpoints (pvc/vm/gpu/snapshot) had zero coverage anywhere
// before #523 — no Go tests, no IQE. These smoke tests are their only net:
// seeded rows must round-trip with exact values through the positional scans.

func setupQualitySubtypeApp() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/quality/pvcs", api.GetPVCRecommendationQuality)
	v1.GET("/recommendations/openshift/quality/vms", api.GetVMRecommendationQuality)
	v1.GET("/recommendations/openshift/quality/gpu", api.GetGPUMIGRecommendationQuality)
	v1.GET("/recommendations/openshift/quality/snapshots", api.GetSnapshotRecommendationQuality)
	return app
}

func seedQualityBase(t *testing.T, pool *pgxpool.Pool, rhID int, orgID, clusterUUID, alias string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, rhID, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
		VALUES ($1, $2::uuid, $3, $4, $5, now()) ON CONFLICT DO NOTHING`,
		rhID, clusterUUID, alias, "src-quality-sub", orgID)
	require.NoError(t, err)
}

func getQuality(t *testing.T, app *echo.Echo, orgID, path string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return rec.Body.Bytes()
}

func TestQualitySubtypes_Smoke(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	const orgID = "org-quality-subtypes"
	const clusterUUID = "44444444-4444-4444-4444-444444444444"
	seedQualityBase(t, pool, 41, orgID, clusterUUID, "quality-sub-cluster")
	now := time.Now().UTC().Truncate(time.Second)

	ctx := context.Background()
	// #779: ensure partitions for every seeded timestamp's month.
	ensureQualityPartitions(t, ctx, pool, now)
	_, err := pool.Exec(ctx, `INSERT INTO pvc_recommendation_quality
		(measured_at, org_id, cluster_uuid, namespace, pvc_name, engine, stability_pct, adoption_detected, days_above_threshold, recommendation_age_hours)
		VALUES ($1, $2, $3::uuid, 'ns-a', 'pvc-a', 'cost', 0.75, true, 5, 48)`,
		now, orgID, clusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO vm_recommendation_quality
		(measured_at, org_id, cluster_uuid, namespace, vm_name, engine, stability_pct, adoption_detected, saturation_days, recommendation_age_hours)
		VALUES ($1, $2, $3::uuid, 'ns-a', 'vm-a', 'cost', NULL, false, NULL, 24)`,
		now, orgID, clusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO gpu_mig_recommendation_quality
		(measured_at, org_id, cluster_uuid, namespace, workload, container_name, engine, stability_pct, adoption_detected, contention_days, recommendation_age_hours)
		VALUES ($1, $2, $3::uuid, 'ns-a', 'wl-a', 'ctr-a', 'cost', 0.5, true, 3, 72)`,
		now, orgID, clusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO snapshot_recommendation_quality
		(measured_at, org_id, cluster_uuid, snapshot_name, adoption_detected, recommendation_age_hours)
		VALUES ($1, $2, $3::uuid, 'snap-a', true, 12)`,
		now, orgID, clusterUUID)
	require.NoError(t, err)

	app := setupQualitySubtypeApp()

	t.Run("pvc exact values incl NULL-free pointers", func(t *testing.T) {
		body := getQuality(t, app, orgID, "/api/cost-management/v1/recommendations/openshift/quality/pvcs")
		var resp struct {
			Data []model.PVCQualityRow `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		require.Equal(t, 1, resp.Meta.Count)
		require.Len(t, resp.Data, 1)
		row := resp.Data[0]
		assert.Equal(t, "pvc-a", row.PVCName)
		assert.Equal(t, "quality-sub-cluster", row.ClusterAlias)
		require.NotNil(t, row.StabilityPct)
		assert.InDelta(t, 0.75, float64(*row.StabilityPct), 0.001)
		assert.True(t, row.AdoptionDetected)
		require.NotNil(t, row.DaysAboveThreshold)
		assert.Equal(t, int64(5), *row.DaysAboveThreshold)
	})

	t.Run("vm NULL stability decodes nil", func(t *testing.T) {
		body := getQuality(t, app, orgID, "/api/cost-management/v1/recommendations/openshift/quality/vms")
		var resp struct {
			Data []model.VMQualityRow `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		require.Equal(t, 1, resp.Meta.Count)
		require.Len(t, resp.Data, 1)
		assert.Equal(t, "vm-a", resp.Data[0].VMName)
		assert.Nil(t, resp.Data[0].StabilityPct)
		assert.Nil(t, resp.Data[0].SaturationDays)
		assert.False(t, resp.Data[0].AdoptionDetected)
	})

	t.Run("gpu mig exact values", func(t *testing.T) {
		body := getQuality(t, app, orgID, "/api/cost-management/v1/recommendations/openshift/quality/gpu")
		var resp struct {
			Data []model.GPUMIGQualityRow `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		require.Equal(t, 1, resp.Meta.Count)
		require.Len(t, resp.Data, 1)
		assert.Equal(t, "ctr-a", resp.Data[0].ContainerName)
		require.NotNil(t, resp.Data[0].ContentionDays)
		assert.Equal(t, int64(3), *resp.Data[0].ContentionDays)
	})

	t.Run("snapshot exact values", func(t *testing.T) {
		body := getQuality(t, app, orgID, "/api/cost-management/v1/recommendations/openshift/quality/snapshots")
		var resp struct {
			Data []model.SnapshotQualityRow `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		require.Equal(t, 1, resp.Meta.Count)
		require.Len(t, resp.Data, 1)
		assert.Equal(t, "snap-a", resp.Data[0].SnapshotName)
		assert.True(t, resp.Data[0].AdoptionDetected)
	})
}

// ensureQualityPartitions creates the monthly partition for every distinct
// month in times on each partitioned quality table (#779). The 000159-000162
// migrations create only the current and next two months, so fixture rows in
// the previous month (first hours of a month, UTC) need explicit partitions —
// pre-fix those inserts failed with SQLSTATE 23514.
func ensureQualityPartitions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, times ...time.Time) {
	t.Helper()
	tables := []string{
		"pvc_recommendation_quality",
		"vm_recommendation_quality",
		"gpu_mig_recommendation_quality",
		"snapshot_recommendation_quality",
	}
	seen := map[string]bool{}
	for _, ts := range times {
		ms := time.Date(ts.UTC().Year(), ts.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
		me := ms.AddDate(0, 1, 0)
		ym := ms.Format("200601")
		if seen[ym] {
			continue
		}
		seen[ym] = true
		for _, table := range tables {
			part := table + "_" + ym
			var exists bool
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1)`, part).Scan(&exists))
			if exists {
				continue
			}
			_, err := pool.Exec(ctx, fmt.Sprintf(
				`CREATE TABLE %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
				part, table, ms.Format("2006-01-02"), me.Format("2006-01-02")))
			require.NoError(t, err)
		}
	}
}

// seedQualityPaginationRows seeds the three pvc_recommendation_quality rows
// the pagination fixture uses (base-2h..base), ensuring partitions for every
// seeded timestamp's month (#779: the 000159-000162 migrations create the
// current and next two months only, so previous-month rows — the first hours
// of a month, UTC — need explicit partitions or the insert fails with 23514).
func seedQualityPaginationRows(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string, base time.Time) {
	t.Helper()
	ctx := context.Background()
	ensureQualityPartitions(t, ctx, pool, base, base.Add(-time.Hour), base.Add(-2*time.Hour))
	for i, name := range []string{"pvc-0", "pvc-1", "pvc-2"} {
		_, err := pool.Exec(ctx, `INSERT INTO pvc_recommendation_quality
			(measured_at, org_id, cluster_uuid, namespace, pvc_name, engine, stability_pct, adoption_detected)
			VALUES ($1, $2, $3::uuid, 'ns-a', $4, 'cost', 0.5, false)`,
			base.Add(time.Duration(-i)*time.Hour), orgID, clusterUUID, name)
		require.NoError(t, err)
	}
}

// TestQualitySubtypes_MonthBoundarySeed (#779): exercises the pagination
// fixture's seeding path at a month boundary — base 00:30 on the 1st puts
// two rows in the previous month, which the 000159-000162 migrations never
// create (current + next 2 months only). Pre-fix: SQLSTATE 23514 on every
// first-of-month CI run. The boundary month's partition is dropped first so
// a leftover from an earlier test cannot mask a missing ensure (the shared
// test DB keeps DDL across tests even though rows are truncated).
func TestQualitySubtypes_MonthBoundarySeed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	const orgID = "org-quality-boundary"
	const clusterUUID = "66666666-6666-6666-6666-666666666666"
	seedQualityBase(t, pool, 46, orgID, clusterUUID, "quality-boundary-cluster")

	n := time.Now().UTC()
	boundary := time.Date(n.Year(), n.Month(), 1, 0, 30, 0, 0, time.UTC)
	ctx := context.Background()

	// Drop the boundary month's partition so this test proves the seeding
	// path creates it itself.
	prevMonth := boundary.Add(-2 * time.Hour)
	_, err := pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS pvc_recommendation_quality_%s`,
		prevMonth.Format("200601")))
	require.NoError(t, err)

	// Same seeding path as the pagination fixture, at the boundary.
	seedQualityPaginationRows(t, pool, orgID, clusterUUID, boundary)

	// The rows must be served by the API. The window is explicit: the quality
	// list's default start-of-month window would exclude the previous-month
	// rows on the 1st, and boundary-2h can be up to a month before "now" on
	// any later day of the month — so start at the boundary month itself.
	app := setupQualitySubtypeApp()
	body := getQuality(t, app, orgID,
		"/api/cost-management/v1/recommendations/openshift/quality/pvcs?limit=10&offset=0&start_date="+
			prevMonth.Format("2006-01-02"))
	var resp struct {
		Data []model.PVCQualityRow `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.Equal(t, 3, resp.Meta.Count,
		"all three boundary-straddling rows served (two previous-month, one current-month)")
}

// Pagination slices must be stable with no dupes or loss across pages (#523:
// OFFSET retained, so pin its contract). Order-independent assertions: pages
// compared as sets, count stable on both.
func TestQualitySubtypes_PaginationStable(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	const orgID = "org-quality-pages"
	const clusterUUID = "55555555-5555-5555-5555-555555555555"
	seedQualityBase(t, pool, 51, orgID, clusterUUID, "quality-pages-cluster")
	now := time.Now().UTC().Truncate(time.Second)

	// Same seeding path the boundary test pins (#777/#779): the helper
	// ensures partitions for every seeded timestamp's month.
	seedQualityPaginationRows(t, pool, orgID, clusterUUID, now)

	// Explicit window: the quality list's default start-of-month window
	// would exclude the now-1h/now-2h rows on the 1st of a month.
	startDate := now.AddDate(0, 0, -3).Format("2006-01-02")

	app := setupQualitySubtypeApp()
	var seen []string
	for _, page := range []string{"limit=2&offset=0", "limit=2&offset=2"} {
		body := getQuality(t, app, orgID, "/api/cost-management/v1/recommendations/openshift/quality/pvcs?"+page+"&start_date="+startDate)
		var resp struct {
			Data []model.PVCQualityRow `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		assert.Equal(t, 3, resp.Meta.Count, "count stable on %s", page)
		for _, row := range resp.Data {
			seen = append(seen, row.PVCName)
		}
	}
	assert.ElementsMatch(t, []string{"pvc-0", "pvc-1", "pvc-2"}, seen, "no dupes or loss across pages")
}

// Same cluster UUID under two tenants: alias join must stay tenant-scoped
// (#523 absorbs the GORM joins #525 deferred). Mirrors the machineset/MIG
// colliding-UUID tests.
func TestQualitySubtypes_CollidingClusterUUIDUsesOwnOrgAlias(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	const orgA = "org-quality-collide-a"
	const orgB = "org-quality-collide-b"
	const sharedUUID = "66666666-6666-6666-6666-666666666666"
	seedQualityBase(t, pool, 61, orgA, sharedUUID, "alias-tenant-a")
	seedQualityBase(t, pool, 62, orgB, sharedUUID, "alias-tenant-b")
	now := time.Now().UTC().Truncate(time.Second)

	ctx := context.Background()
	// #779 (deferred minor): plain-`now` seeds are always covered by the
	// migration's current+2-month window, but ensure anyway so every quality
	// fixture follows one rule.
	ensureQualityPartitions(t, ctx, pool, now)
	_, err := pool.Exec(ctx, `INSERT INTO pvc_recommendation_quality
		(measured_at, org_id, cluster_uuid, namespace, pvc_name, engine, stability_pct, adoption_detected)
		VALUES ($1, $2, $3::uuid, 'ns-a', 'pvc-a', 'cost', 0.5, false)`,
		now, orgA, sharedUUID)
	require.NoError(t, err)

	app := setupQualitySubtypeApp()
	body := getQuality(t, app, orgA, "/api/cost-management/v1/recommendations/openshift/quality/pvcs")
	var resp struct {
		Data []model.PVCQualityRow `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "alias-tenant-a", resp.Data[0].ClusterAlias)
}
