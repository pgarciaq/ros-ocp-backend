package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #853: EXPLAIN (ANALYZE, BUFFERS) verification of the #842 eligibility
// EXISTS at org scale, using the ACTUAL serving-layer SQL. The test:
//
//   - seeds ~50K container identities (+ ~5K namespaces, 2 term rows per
//     identity — the scale-benchmark ceiling), ANALYZEs for realistic
//     planner statistics;
//   - captures every SQL statement the real list functions emit through a
//     recording connection pool (verbatim text + bind args — never a
//     hand-reconstruction);
//   - EXPLAINs every captured query containing the eligibility EXISTS —
//     both entities × (default date-range request, explicit filter);
//   - asserts only the STRUCTURAL guard: the EXISTS inner side must use
//     an index (never a Seq Scan on the aliased inner table). Timing is
//     recorded as evidence, never asserted (flaky by nature).
//
// The plans are t.Log'd — they are the evidence posted to #853.

type capturedQuery struct {
	sqlText string
	args    []interface{}
}

// recordingDB wraps *sql.DB, recording every statement gorm sends. The
// embedded *sql.DB satisfies gorm's ConnPool; Query/Exec are intercepted.
type recordingDB struct {
	*sql.DB
	captured []capturedQuery
}

func (r *recordingDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	r.captured = append(r.captured, capturedQuery{sqlText: query, args: args})
	return r.DB.QueryContext(ctx, query, args...)
}

func (r *recordingDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	r.captured = append(r.captured, capturedQuery{sqlText: query, args: args})
	return r.DB.ExecContext(ctx, query, args...)
}

func TestEligibilityExistsExplain_OrgScale(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	pool := testutil.SetupTestDB(t)
	// The account/cluster rows the fixture identities hang from.
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'explain-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	seedOrgScale(t, pool)

	// A gorm instance over a recording pool: every serving-layer
	// statement is captured verbatim with its bind args.
	sqlDB := stdlib.OpenDBFromPool(pool)
	rec := &recordingDB{DB: sqlDB}
	gormDB, gormErr := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: rec}), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, gormErr)
	prevDB := database.DB
	database.DB = gormDB
	t.Cleanup(func() { database.DB = prevDB })

	now := time.Now()
	dateFrom := now.AddDate(0, 0, -30)
	dateTo := now.AddDate(0, 0, 1)

	cases := []struct {
		name  string
		ns    bool // namespace list (false = container)
		query map[string]interface{}
	}{
		{
			name: "container default (date range)",
			query: map[string]interface{}{
				"rs.stale = ?":       false,
				"rs.updated_at >= ?": dateFrom,
				"rs.updated_at < ?":  dateTo,
			},
		},
		{
			name: "container explicit filter",
			query: map[string]interface{}{
				"rs.stale = ?":     false,
				"rs.category IN ?": []string{"zombie"},
				"rs.engine IN ?":   []string{"cost"},
			},
		},
		{
			name: "namespace default (date range)",
			ns:   true,
			query: map[string]interface{}{
				"ns.stale = ?":                false,
				"ns.monitoring_end_time >= ?": dateFrom,
				"ns.monitoring_end_time < ?":  dateTo,
			},
		},
		{
			name: "namespace explicit filter",
			ns:   true,
			query: map[string]interface{}{
				"ns.stale = ?":   false,
				"ns.engine IN ?": []string{"cost"},
				"ns.term IN ?":   []string{"short"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(rec.captured)
			opts := listoptions.ListOptions{Limit: 50, OrderBy: "recommendation_sets.namespace", OrderHow: listoptions.OrderAsc}
			if tc.ns {
				opts = listoptions.ListOptions{Limit: 50, OrderBy: "namespace_recommendation_sets.namespace_name", OrderHow: listoptions.OrderAsc}
				_, err := GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, tc.query, map[string][]string{"*": {}}, money.DefaultCurrency)
				require.NoError(t, err)
			} else {
				_, err := GetNativeRecommendations(testutil.TestOrgID, opts, tc.query, map[string][]string{"*": {}}, money.DefaultCurrency)
				require.NoError(t, err)
			}

			explained := 0
			for _, q := range rec.captured[before:] {
				if !strings.Contains(q.sqlText, "EXISTS") {
					continue
				}
				explained++
				plan := explainAnalyze(t, pool, q)
				t.Logf("=== #853 %s — captured SQL:\n%s\nargs: %v\n=== plan:\n%s",
					tc.name, q.sqlText, q.args, plan)
				// The structural guard: the EXISTS inner side (aliased
				// rs / ns) must never degrade to a sequential scan —
				// that is the regression this test watches for (an
				// index dropped or a planner flip at scale).
				assert.NotContains(t, plan, "Seq Scan on recommendation_sets rs",
					"the container eligibility EXISTS must probe recommendation_sets via an index, not a sequential scan")
				assert.NotContains(t, plan, "Seq Scan on namespace_recommendation_sets ns",
					"the namespace eligibility EXISTS must probe namespace_recommendation_sets via an index, not a sequential scan")
			}
			assert.Positive(t, explained, "the case must exercise the eligibility EXISTS (detail predicates present)")
		})
	}
}

// seedOrgScale seeds the org-scale fixture: ~50K container identities
// (2 term rows each in recommendation_sets), ~5K namespaces (2 rows each
// in namespace_recommendation_sets), then ANALYZEs — the scale-benchmark
// ceiling (4K–50K+ workloads).
func seedOrgScale(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	const namespaces = 100
	const workloadsPerNS = 500 // 50K container identities
	const nsNamespaces = 5000

	containers := make([][]interface{}, 0, namespaces*workloadsPerNS)
	rsRows := make([][]interface{}, 0, namespaces*workloadsPerNS*2)
	for ns := 0; ns < namespaces; ns++ {
		nsName := fmt.Sprintf("ns-%04d", ns)
		for wl := 0; wl < workloadsPerNS; wl++ {
			wlName := fmt.Sprintf("wl-%04d", wl)
			containers = append(containers, []interface{}{
				testutil.TestOrgID, testutil.TestClusterUUID, nsName, wlName, testutil.TestWorkloadType, "c0",
			})
			for _, term := range []string{"short", "medium"} {
				rsRows = append(rsRows, []interface{}{
					testutil.TestOrgID, testutil.TestClusterUUID, nsName, wlName, testutil.TestWorkloadType, "c0",
					term, "cost", time.Now().UTC(),
				})
			}
		}
	}
	copyRows(t, pool, "org_container_keys",
		[]string{"org_id", "cluster_uuid", "namespace", "workload", "workload_type", "container_name"}, containers)
	copyRows(t, pool, "recommendation_sets",
		[]string{"org_id", "cluster_uuid", "namespace", "workload", "workload_type", "container_name", "term", "engine", "updated_at"}, rsRows)

	nsKeys := make([][]interface{}, 0, nsNamespaces)
	nsRows := make([][]interface{}, 0, nsNamespaces*2)
	for i := 0; i < nsNamespaces; i++ {
		nsName := fmt.Sprintf("nspace-%04d", i)
		nsKeys = append(nsKeys, []interface{}{testutil.TestOrgID, testutil.TestClusterUUID, nsName})
		for _, term := range []string{"short", "medium"} {
			nsRows = append(nsRows, []interface{}{
				testutil.TestOrgID, testutil.TestClusterUUID, nsName, term, "cost",
				time.Now().UTC(), time.Now().UTC().Add(24 * time.Hour), time.Now().UTC(),
			})
		}
	}
	copyRows(t, pool, "org_namespace_keys",
		[]string{"org_id", "cluster_uuid", "namespace_name"}, nsKeys)
	copyRows(t, pool, "namespace_recommendation_sets",
		[]string{"org_id", "cluster_uuid", "namespace_name", "term", "engine", "monitoring_start_time", "monitoring_end_time", "updated_at"}, nsRows)

	// Realistic planner statistics — without ANALYZE the planner works
	// from defaults and the plan is not representative.
	_, err := pool.Exec(ctx, `ANALYZE org_container_keys; ANALYZE recommendation_sets; ANALYZE org_namespace_keys; ANALYZE namespace_recommendation_sets`)
	require.NoError(t, err)
}

func copyRows(t *testing.T, pool *pgxpool.Pool, table string, columns []string, rows [][]interface{}) {
	t.Helper()
	if len(rows) == 0 {
		return
	}
	// Chunk to keep COPY memory bounded.
	for start := 0; start < len(rows); start += 10000 {
		end := start + 10000
		if end > len(rows) {
			end = len(rows)
		}
		src := pgx.CopyFromRows(rows[start:end])
		_, err := pool.CopyFrom(context.Background(), pgx.Identifier{table}, columns, src)
		require.NoError(t, err)
	}
}

func explainAnalyze(t *testing.T, pool *pgxpool.Pool, q capturedQuery) string {
	t.Helper()
	args := make([]interface{}, len(q.args))
	copy(args, q.args)
	rows, err := pool.Query(context.Background(), "EXPLAIN (ANALYZE, BUFFERS) "+q.sqlText, args...)
	require.NoError(t, err)
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	require.NoError(t, rows.Err())
	return sb.String()
}
