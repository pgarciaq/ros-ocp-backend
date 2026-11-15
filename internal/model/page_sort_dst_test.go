package model_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
)

// #851: the page-sort value is cast (ts)::text, and timestamptz renders in the
// SESSION timezone with a variable UTC offset. Across the America/New_York
// fall-back boundary (2026-11-01, 02:00 EDT -> 01:00 EST) the local hour
// 01:00-01:59 occurs twice: an instant from the first pass renders with -04
// and the same local clock time from the second pass with -05. Text ordering
// compares the local clock before the offset, so it can disagree with
// chronological ordering.
//
// Fixture instants (chronological A < B, but B's text render sorts first):
//
//	dstInstantA = 2026-11-01 05:30:00+00 -> renders "2026-11-01 01:30:00-04"
//	dstInstantB = 2026-11-01 06:15:00+00 -> renders "2026-11-01 01:15:00-05"
//
// The page subquery orders by the text render, so on the unfixed code ASC
// returns B before A. The fix renders timestamps as stable UTC text
// (to_char(ts AT TIME ZONE 'UTC', ...)), restoring chronological order.
const (
	dstInstantA = "2026-11-01 05:30:00+00"
	dstInstantB = "2026-11-01 06:15:00+00"

	dstClusterA  = "51510a01-0000-0000-0000-000000000001"
	dstClusterB  = "51510a02-0000-0000-0000-000000000002"
	dstNSAlpha   = "ns-dst-alpha"
	dstNSBeta    = "ns-dst-beta"
	dstContainer = "container-dst-b"
)

// openNYSessionGORM points database.DB at a pool whose every connection
// renders timestamptz in America/New_York (a DST-observing zone). Returns the
// shared UTC pool for seeding — the timezone only matters for the rendering
// inside the queries under test.
//
// NOTE: testutil.OpenTestGORM is a sync.Once keyed on the first pool — it
// would silently return the shared (UTC) gorm — so the NY gorm DB is built
// directly here.
func openNYSessionGORM(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SetupTestDB(t)
	cfg := *shared.Config()
	cfg.ConnConfig = shared.Config().ConnConfig.Copy()
	cfg.ConnConfig.RuntimeParams = map[string]string{"timezone": "America/New_York"}
	cfg.MaxConns = 4
	cfg.MinConns = 0
	nyPool, err := pgxpool.NewWithConfig(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(nyPool.Close)

	sqlDB := stdlib.OpenDBFromPool(nyPool)
	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gdb
	return shared
}

// seedDSTNamespaceFixture builds the namespace-list fixture: two clusters,
// one namespace each, whose clusters.last_reported_at sit on either side of
// the fall-back boundary (cluster A at 05:30Z, cluster B at 06:15Z). The nrs
// rows are seeded directly (the established keys-path fixture pattern) —
// last_reported is a clusters column, so it does not depend on the nrs rows.
func seedDSTNamespaceFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'dst-cluster-a', 'src-a', $2), (1, $3, 'dst-cluster-b', 'src-b', $4)
		ON CONFLICT DO NOTHING`, dstClusterA, dstInstantA, dstClusterB, dstInstantB)
	require.NoError(t, err)

	recs := []namespace.NamespaceRec{
		{OrgID: testutil.TestOrgID, ClusterUUID: dstClusterA, Namespace: dstNSAlpha, Term: "short", Engine: "cost", RecCPURequestMC: 100},
		{OrgID: testutil.TestOrgID, ClusterUUID: dstClusterB, Namespace: dstNSBeta, Term: "short", Engine: "cost", RecCPURequestMC: 110},
	}
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, recs))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))
}

// seedDSTContainerFixture builds the container-list fixture: two containers
// whose recommendation_sets.updated_at sit on either side of the fall-back
// boundary (the first container at 05:30Z, the second at 06:15Z).
func seedDSTContainerFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'dst-cluster-a', 'src-a', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate:       start,
		OrgID:            testutil.TestOrgID,
		ClusterUUID:      testutil.TestClusterUUID,
		Namespace:        testutil.TestNamespace,
		Workload:         "workload-dst",
		WorkloadType:     testutil.TestWorkloadType,
		ContainerName:    dstContainer,
		CPURequestP50MC:  180,
		CPURequestP95MC:  210,
		CPUUsageP50MC:    170,
		CPUUsageP95MC:    200,
		MemRequestP50KiB: 512000,
		MemRequestP95KiB: 524288,
		MemUsageP50KiB:   500000,
		MemUsageP95KiB:   520000,
	})
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	// The engine stamps updated_at = now(); pin the two containers to the
	// fall-back boundary instants.
	_, err = pool.Exec(ctx, `UPDATE recommendation_sets SET updated_at = $2 WHERE container_name = $1`, testutil.TestContainer, dstInstantA)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE recommendation_sets SET updated_at = $2 WHERE container_name = $1`, dstContainer, dstInstantB)
	require.NoError(t, err)
}

// nsListPage fetches one namespace-list page (limit 1) and cursor-continues to
// a second, returning the namespace names in served order.
func nsListPage(t *testing.T, orderHow string, query map[string]interface{}) []string {
	t.Helper()
	opts := listoptions.ListOptions{
		Limit:    1,
		OrderBy:  "clusters.last_reported_at",
		OrderHow: orderHow,
	}
	page1, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, query, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page1.Results, 1)
	require.True(t, page1.HasNext)
	require.NotNil(t, page1.LastAnchor)

	opts.HasCursor = true
	opts.AfterNamespaceName = page1.LastAnchor.Namespace
	opts.AfterNSClusterUUID = page1.LastAnchor.ClusterUUID
	opts.AfterNSSortPresent = true
	opts.AfterNSSortValue = page1.LastAnchor.SortValue
	page2, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, query, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page2.Results, 1)

	return []string{page1.Results[0].Project, page2.Results[0].Project}
}

// containerListPage fetches one container-list page (limit 1) and
// cursor-continues to a second, returning the container names in served order.
func containerListPage(t *testing.T, orderHow string) []string {
	t.Helper()
	opts := listoptions.ListOptions{
		Limit:    1,
		OrderBy:  "recommendation_sets.updated_at",
		OrderHow: orderHow,
	}
	page1, err := model.GetNativeRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page1.Results, 1)
	require.True(t, page1.HasNext)
	require.NotNil(t, page1.LastAnchor)

	opts.HasCursor = true
	opts.AfterNamespace = page1.LastAnchor.Namespace
	opts.AfterWorkload = page1.LastAnchor.Workload
	opts.AfterWorkloadType = page1.LastAnchor.WorkloadType
	opts.AfterContainer = page1.LastAnchor.ContainerName
	opts.AfterContainerClusterUUID = page1.LastAnchor.ClusterUUID
	opts.AfterContainerSortPresent = true
	opts.AfterContainerSortValue = page1.LastAnchor.SortValue
	page2, err := model.GetNativeRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page2.Results, 1)

	return []string{page1.Results[0].Container, page2.Results[0].Container}
}

// TestNSKeysPath_LastReportedAcrossDSTBoundary pins #851 on the namespace
// keys path (routed via ns.stale=false): ordering by last_reported across the
// fall-back boundary must be CHRONOLOGICAL, not the timestamptz::text render
// order — and cursor continuation must agree (no skip, no duplicate).
func TestNSKeysPath_LastReportedAcrossDSTBoundary(t *testing.T) {
	pool := openNYSessionGORM(t)
	seedDSTNamespaceFixture(t, pool)

	// ASC: 05:30Z (renders 01:30-04) before 06:15Z (renders 01:15-05).
	assert.Equal(t, []string{dstNSAlpha, dstNSBeta}, nsListPage(t, listoptions.OrderAsc, map[string]interface{}{"ns.stale = ?": false}),
		"ASC by last_reported must be chronological across the fall-back boundary; the text render (01:15-05 < 01:30-04) reorders it (#851)")

	// DESC: the reverse.
	assert.Equal(t, []string{dstNSBeta, dstNSAlpha}, nsListPage(t, listoptions.OrderDesc, map[string]interface{}{"ns.stale = ?": false}),
		"DESC by last_reported must be reverse-chronological across the fall-back boundary (#851)")
}

// TestNSDistinctPath_LastReportedAcrossDSTBoundary_Control is the control:
// the NS DISTINCT fallback orders by the TYPED ros_ns_page_sort_raw, so it is
// chronological on the unfixed code AND must remain so after the fix (the
// fallback is out of #851's scope by owner decision).
func TestNSDistinctPath_LastReportedAcrossDSTBoundary_Control(t *testing.T) {
	pool := openNYSessionGORM(t)
	seedDSTNamespaceFixture(t, pool)

	assert.Equal(t, []string{dstNSAlpha, dstNSBeta}, nsListPage(t, listoptions.OrderAsc, nil),
		"the DISTINCT fallback (typed sort) must stay chronological across the boundary")
	assert.Equal(t, []string{dstNSBeta, dstNSAlpha}, nsListPage(t, listoptions.OrderDesc, nil),
		"the DISTINCT fallback (typed sort) must stay reverse-chronological across the boundary")
}

// TestContainerKeysPath_LastReportedAcrossDSTBoundary pins #851 on the
// container org-keys path (the always-live container route): ordering by
// last_reported (rs.updated_at) across the fall-back boundary must be
// chronological, with cursor continuation agreeing.
func TestContainerKeysPath_LastReportedAcrossDSTBoundary(t *testing.T) {
	pool := openNYSessionGORM(t)
	seedDSTContainerFixture(t, pool)

	assert.Equal(t, []string{testutil.TestContainer, dstContainer}, containerListPage(t, listoptions.OrderAsc),
		"ASC by last_reported must be chronological across the fall-back boundary; the text render (01:15-05 < 01:30-04) reorders it (#851)")
	assert.Equal(t, []string{dstContainer, testutil.TestContainer}, containerListPage(t, listoptions.OrderDesc),
		"DESC by last_reported must be reverse-chronological across the fall-back boundary (#851)")
}

// TestContainerKeysPath_SubSecondOrdering pins the fixed-width (.US) part of
// the stable UTC page-sort text: two containers in the SAME second but
// microsecond apart must order chronologically, with the identity tie-break
// NOT consulted. The fixture's identity order is deliberately reversed
// relative to chronological order (zzz-sub earlier than aaa-sub), so a
// variable-width or second-truncating render would fall through to the
// tie-break and serve the wrong order. This is a regression guard for the
// fix's format — sub-second ::text renders are already chronological, so it
// also passes on the pre-#851 code under a UTC session.
func TestContainerKeysPath_SubSecondOrdering(t *testing.T) {
	pool := openNYSessionGORM(t)
	seedDSTSubSecondContainerFixture(t, pool)

	// ASC: zzz-sub (05:30:00.000001Z) before aaa-sub (05:30:00.000002Z)
	// despite the identity tie-break preferring aaa-sub.
	opts := listoptions.ListOptions{
		Limit:    2,
		OrderBy:  "recommendation_sets.updated_at",
		OrderHow: listoptions.OrderAsc,
	}
	page, err := model.GetNativeRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page.Results, 2)
	assert.Equal(t, "zzz-sub", page.Results[0].Container,
		"sub-second values must order chronologically — a second-truncating render falls through to the identity tie-break and reorders (#851)")
	assert.Equal(t, "aaa-sub", page.Results[1].Container)

	// DESC: the reverse.
	opts.OrderHow = listoptions.OrderDesc
	page, err = model.GetNativeRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page.Results, 2)
	assert.Equal(t, "aaa-sub", page.Results[0].Container)
	assert.Equal(t, "zzz-sub", page.Results[1].Container)
}

// seedDSTSubSecondContainerFixture seeds two containers whose updated_at sit
// one microsecond apart inside the same second, with identity order reversed
// relative to chronological order.
func seedDSTSubSecondContainerFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'dst-cluster-a', 'src-a', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for _, spec := range []struct {
		workload, container string
	}{
		{"workload-sub-zzz", "zzz-sub"},
		{"workload-sub-aaa", "aaa-sub"},
	} {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       start,
			OrgID:            testutil.TestOrgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         spec.workload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    spec.container,
			CPURequestP50MC:  180,
			CPURequestP95MC:  210,
			CPUUsageP50MC:    170,
			CPUUsageP95MC:    200,
			MemRequestP50KiB: 512000,
			MemRequestP95KiB: 524288,
			MemUsageP50KiB:   500000,
			MemUsageP95KiB:   520000,
		})
	}
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	_, err = pool.Exec(ctx, `UPDATE recommendation_sets SET updated_at = $2 WHERE container_name = $1`, "zzz-sub", "2026-11-01 05:30:00.000001+00")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE recommendation_sets SET updated_at = $2 WHERE container_name = $1`, "aaa-sub", "2026-11-01 05:30:00.000002+00")
	require.NoError(t, err)
}
