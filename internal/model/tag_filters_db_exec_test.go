package model_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	gorm "gorm.io/gorm"
)

// #815's executed db-mode tests: the tenant schema (with Koku's reporting
// tables) is created ad-hoc on the shared testcontainer — the reporting
// tables do not exist in ROS's migrations (db mode reads Koku's database in
// shared-DB deployments).

const (
	dbTagClusterA = "71510a01-0000-0000-0000-000000000001"
	dbTagClusterB = "71510a02-0000-0000-0000-000000000002"
	dbTagClusterC = "71510a03-0000-0000-0000-000000000003"
	dbTagNSA      = "ns-dbtag-a"
	dbTagNSB      = "ns-dbtag-b"
	dbTagNSC      = "ns-dbtag-c"
)

// setupKokuLabelSchema creates the tenant schema with the five Koku tables
// the db-mode tag predicate reads (label summaries, report periods, enabled
// tag keys) plus reporting_ocptags_values (the pre-#815 source, kept so the
// RED can seed the world as summarization leaves it).
func setupKokuLabelSchema(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	schema, err := tags.TenantSchema(testutil.TestOrgID)
	require.NoError(t, err)
	// The per-test truncate only touches the public schema — the tenant
	// schema persists across tests, so drop it for a clean fixture.
	_, err = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		CREATE SCHEMA `+schema+`;
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_enabledtagkeys (
			uuid UUID PRIMARY KEY,
			key VARCHAR(512) NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT true,
			provider_type VARCHAR(50) NOT NULL,
			UNIQUE (key, provider_type)
		);
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocptags_values (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			cluster_ids TEXT[] NOT NULL,
			cluster_aliases TEXT[] NOT NULL DEFAULT '{}',
			namespaces TEXT[] NOT NULL,
			nodes TEXT[],
			UNIQUE (key, value)
		);
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocpusagereportperiod (
			id BIGINT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			report_period_start DATE NOT NULL
		);
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocpusagepodlabel_summary (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			values TEXT[] NOT NULL,
			report_period_id BIGINT NOT NULL REFERENCES `+schema+`.reporting_ocpusagereportperiod (id),
			namespace TEXT NOT NULL,
			node TEXT
		);
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocpstoragevolumelabel_summary (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			values TEXT[] NOT NULL,
			report_period_id BIGINT NOT NULL REFERENCES `+schema+`.reporting_ocpusagereportperiod (id),
			namespace TEXT NOT NULL,
			node TEXT
		);
	`)
	require.NoError(t, err)
	return schema
}

// dbTagMonthStarts returns (lastMonthStart, thisMonthStart) — the ADR-0353
// month-snap window bounds.
func dbTagMonthStarts() (time.Time, time.Time) {
	now := time.Now().UTC()
	thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return thisMonthStart.AddDate(0, -1, 0), thisMonthStart
}

// seedDBTagTwoClusterWorld builds the issue's reproduction shape: two
// clusters sharing environment=production. The label summaries are the
// ground truth (both clusters); reporting_ocptags_values is what
// summarization leaves behind — the LAST-summarized cluster (B) owns the
// arrays, cluster A is gone from them.
func seedDBTagTwoClusterWorld(t *testing.T, pool *pgxpool.Pool, schema string) {
	t.Helper()
	ctx := context.Background()
	lastMonthStart, thisMonthStart := dbTagMonthStarts()

	_, err := pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_enabledtagkeys (uuid, key, enabled, provider_type)
		VALUES (gen_random_uuid(), 'environment', true, 'OCP') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_ocpusagereportperiod (id, cluster_id, report_period_start)
		VALUES (1, $1, $3), (2, $2, $4), (3, $5, $6)`,
		dbTagClusterA, dbTagClusterB, thisMonthStart, lastMonthStart, dbTagClusterC, lastMonthStart.AddDate(0, -2, 0))
	require.NoError(t, err)

	// The label summaries: ground truth — BOTH clusters carry the tag
	// (cluster C too, but its only period is three months old — outside
	// the ADR-0353 month-snapped window).
	_, err = pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_ocpusagepodlabel_summary (uuid, key, values, report_period_id, namespace, node)
		VALUES
			(gen_random_uuid(), 'environment', ARRAY['production'], 1, $1, 'node-a1'),
			(gen_random_uuid(), 'environment', ARRAY['production'], 2, $2, 'node-b1'),
			(gen_random_uuid(), 'environment', ARRAY['production'], 3, $3, 'node-c1')`,
		dbTagNSA, dbTagNSB, dbTagNSC)
	require.NoError(t, err)

	// The tags_values arrays as summarization leaves them: cluster B
	// summarized last and OWNS the shared key/value — cluster A is gone.
	_, err = pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_ocptags_values (uuid, key, value, cluster_ids, namespaces)
		VALUES (gen_random_uuid(), 'environment', 'production', ARRAY[$1], ARRAY[$2])`,
		dbTagClusterB, dbTagNSB)
	require.NoError(t, err)

	// org_container_keys rows for the clusters' namespaces.
	_, err = pool.Exec(ctx, `INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
		VALUES
			($3, $1, $4, 'w-a', 'Deployment', 'c-a'),
			($3, $2, $5, 'w-b', 'Deployment', 'c-b'),
			($3, $6, $7, 'w-c', 'Deployment', 'c-c')
		ON CONFLICT DO NOTHING`,
		dbTagClusterA, dbTagClusterB, testutil.TestOrgID, dbTagNSA, dbTagNSB, dbTagClusterC, dbTagNSC)
	require.NoError(t, err)
}

// dbTagFilteredPairs seeds the two-cluster world and runs the keys filter in
// the given tags source mode, returning the matched (cluster, namespace)
// pairs. Owns its test setup — do not combine with a direct SetupTestDB
// call in the same test (the shared-DB mutex is held per test).
func dbTagFilteredPairs(t *testing.T, source string, filters []model.TagFilter) [][2]string {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	gdb := testutil.OpenTestGORM(pool)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)
	return dbTagRunKeysFilter(t, gdb, source, filters)
}

// dbTagRunKeysFilter runs the keys filter over the seeded ock rows in the
// given tags source mode. Callers must have called SetupTestDB already.
func dbTagRunKeysFilter(t *testing.T, gdb *gorm.DB, source string, filters []model.TagFilter) [][2]string {
	t.Helper()
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", source)

	query := gdb.Table("org_container_keys ock").
		Select("ock.cluster_uuid::text, ock.namespace").
		Where("ock.org_id = ?", testutil.TestOrgID)
	query = model.ApplyTagFiltersToKeys(query, testutil.TestOrgID, filters)

	rows, err := query.Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var pairs [][2]string
	for rows.Next() {
		var cluster, ns string
		require.NoError(t, rows.Scan(&cluster, &ns))
		pairs = append(pairs, [2]string{cluster, ns})
	}
	return pairs
}

// TestApplyTagFiltersToKeys_DBMode_SharedValueMatchesBothClusters is #815's
// RED: two clusters share environment=production; the label summaries (the
// ground truth) carry both, but the tags_values arrays — the pre-#815
// source, seeded as summarization leaves them — hold only the
// last-summarized cluster. The db-mode filter must match BOTH clusters:
// dropping cluster A is the silent cluster exclusion the issue reproduces.
func TestApplyTagFiltersToKeys_DBMode_SharedValueMatchesBothClusters(t *testing.T) {
	pairs := dbTagFilteredPairs(t, "db", []model.TagFilter{{Key: "environment", Values: []string{"production"}}})

	require.Len(t, pairs, 2,
		"both clusters carry environment=production in the label summaries — the filter must match both")
	assert.Contains(t, pairs, [2]string{dbTagClusterA, dbTagNSA},
		"cluster A is silently excluded: the tags_values arrays hold only the last-summarized cluster (#815)")
	assert.Contains(t, pairs, [2]string{dbTagClusterB, dbTagNSB})
}

// TestApplyTagFiltersToKeys_DBMode_WindowMirrorsPush pins the ADR-0353
// month-snap: a cluster keeps matching while one of its report periods is
// in [last_month_start, this_month_end] — cluster B's only period is last
// month (matches); a cluster whose only period is three months old must NOT
// match (the same staleness profile as the api-mode push, so the two modes
// agree).
func TestApplyTagFiltersToKeys_DBMode_WindowMirrorsPush(t *testing.T) {
	pairs := dbTagFilteredPairs(t, "db", []model.TagFilter{{Key: "environment", Values: []string{"production"}}})
	assert.Contains(t, pairs, [2]string{dbTagClusterB, dbTagNSB},
		"a cluster whose period is last month (inside the window) must match")
	for _, p := range pairs {
		assert.NotEqual(t, dbTagClusterC, p[0],
			"a cluster whose only period is three months old is outside the month-snapped window — it must not match")
	}
}

// TestApplyTagFiltersToKeys_DBMode_StorageLabelsMatch pins that BOTH label
// sources feed the predicate: a key present only in the storage-volume
// summary matches (reporting_ocptags_values was fed by both the pod and the
// storage SQL — the replacement must not regress the storage half).
func TestApplyTagFiltersToKeys_DBMode_StorageLabelsMatch(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	gdb := testutil.OpenTestGORM(pool)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_enabledtagkeys (uuid, key, enabled, provider_type)
		VALUES (gen_random_uuid(), 'zone', true, 'OCP') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_ocpstoragevolumelabel_summary (uuid, key, values, report_period_id, namespace, node)
		VALUES (gen_random_uuid(), 'zone', ARRAY['us-east'], 1, $1, NULL)`, dbTagNSA)
	require.NoError(t, err)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	query := gdb.Table("org_container_keys ock").
		Select("ock.cluster_uuid::text, ock.namespace").
		Where("ock.org_id = ?", testutil.TestOrgID)
	query = model.ApplyTagFiltersToKeys(query, testutil.TestOrgID, []model.TagFilter{{Key: "zone", Values: []string{"us-east"}}})

	rows, err := query.Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var got [][2]string
	for rows.Next() {
		var cluster, ns string
		require.NoError(t, rows.Scan(&cluster, &ns))
		got = append(got, [2]string{cluster, ns})
	}
	assert.Equal(t, [][2]string{{dbTagClusterA, dbTagNSA}}, got,
		"a key/value present only in the storage-volume label summary must match its cluster")
}

// TestApplyTagFiltersToKeys_DBMode_DisabledKeyStopsMatchingImmediately pins
// the folded disable fix (the owner's #815 comment): a key disabled in
// reporting_enabledtagkeys stops matching at query time — no summarization
// run (which is what purges the label rows) required.
func TestApplyTagFiltersToKeys_DBMode_DisabledKeyStopsMatchingImmediately(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	gdb := testutil.OpenTestGORM(pool)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)

	// Disable the key — in the pre-#815 world the label rows persist until
	// the next summarization purges them; the query-time EXISTS must not wait.
	_, err := pool.Exec(context.Background(), `UPDATE `+schema+`.reporting_enabledtagkeys SET enabled = false WHERE key = 'environment'`)
	require.NoError(t, err)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	query := gdb.Table("org_container_keys ock").
		Select("ock.cluster_uuid::text, ock.namespace").
		Where("ock.org_id = ?", testutil.TestOrgID)
	query = model.ApplyTagFiltersToKeys(query, testutil.TestOrgID, []model.TagFilter{{Key: "environment", Values: []string{"production"}}})

	rows, err := query.Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	assert.False(t, rows.Next(),
		"a disabled tag key must stop matching immediately in db mode — not wait for the next summarization purge (#815)")
}

// TestApplyTagFiltersToKeys_WildcardMatchesAnyValue pins the wildcard: a
// filter with values ["*"] matches any value for the key.
func TestApplyTagFiltersToKeys_DBMode_WildcardMatchesAnyValue(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	gdb := testutil.OpenTestGORM(pool)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)
	// Cluster A's value is staging, not production — the wildcard must still match it.
	_, err := pool.Exec(context.Background(),
		`UPDATE `+schema+`.reporting_ocpusagepodlabel_summary SET values = ARRAY['staging'] WHERE report_period_id = 1`)
	require.NoError(t, err)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	query := gdb.Table("org_container_keys ock").
		Select("ock.cluster_uuid::text, ock.namespace").
		Where("ock.org_id = ?", testutil.TestOrgID)
	query = model.ApplyTagFiltersToKeys(query, testutil.TestOrgID, []model.TagFilter{{Key: "environment", Values: []string{"*"}}})

	rows, err := query.Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var got [][2]string
	for rows.Next() {
		var cluster, ns string
		require.NoError(t, rows.Scan(&cluster, &ns))
		got = append(got, [2]string{cluster, ns})
	}
	assert.Len(t, got, 2, "the wildcard must match any value for the key — both clusters carry environment")
}

// TestApplyTagFiltersToKeys_DBMode_DisableScopedToOCP pins the #815 review
// P2 fix: Cost Management enables tag keys per provider type — a key
// disabled for OCP must stop matching even when an AWS-enabled row of the
// same name exists in the tenant schema.
func TestApplyTagFiltersToKeys_DBMode_DisableScopedToOCP(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	gdb := testutil.OpenTestGORM(pool)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)
	ctx := context.Background()

	// The key is disabled for OCP but enabled for AWS.
	_, err := pool.Exec(ctx, `UPDATE `+schema+`.reporting_enabledtagkeys SET enabled = false WHERE key = 'environment' AND provider_type = 'OCP'`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_enabledtagkeys (uuid, key, enabled, provider_type)
		VALUES (gen_random_uuid(), 'environment', true, 'AWS') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	query := gdb.Table("org_container_keys ock").
		Select("ock.cluster_uuid::text, ock.namespace").
		Where("ock.org_id = ?", testutil.TestOrgID)
	query = model.ApplyTagFiltersToKeys(query, testutil.TestOrgID, []model.TagFilter{{Key: "environment", Values: []string{"production"}}})

	rows, err := query.Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	assert.False(t, rows.Next(),
		"a key disabled for OCP must not keep matching via an AWS-enabled row of the same name (#815 review P2)")
}

// TestApplyTagFiltersToClusterNamespace_DBMode_Executed is executed
// coverage for the parameterized-columns gorm site (the namespace/history
// routes) — the #815 review found it covered only by a ToSQL shape test.
func TestApplyTagFiltersToClusterNamespace_DBMode_Executed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	gdb := testutil.OpenTestGORM(pool)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	query := gdb.Table("org_container_keys ock").
		Select("ock.cluster_uuid::text, ock.namespace").
		Where("ock.org_id = ?", testutil.TestOrgID)
	query = model.ApplyTagFiltersToClusterNamespace(query, testutil.TestOrgID,
		[]model.TagFilter{{Key: "environment", Values: []string{"production"}}},
		"ock.cluster_uuid", "ock.namespace")

	rows, err := query.Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var got [][2]string
	for rows.Next() {
		var cluster, ns string
		require.NoError(t, rows.Scan(&cluster, &ns))
		got = append(got, [2]string{cluster, ns})
	}
	assert.ElementsMatch(t, [][2]string{{dbTagClusterA, dbTagNSA}, {dbTagClusterB, dbTagNSB}}, got,
		"the parameterized-columns site must match both in-window clusters and exclude the out-of-window one")
}

// TestApplyTagFiltersToKeys_DBAndAPIModesAgree is the issue's close
// criterion 3, asserted: the same org data seeded both ways — label
// summaries for db mode, org_container_keys.resolved_tags for api mode —
// the two predicates select the same (cluster, namespace) set.
func TestApplyTagFiltersToKeys_DBAndAPIModesAgree(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	gdb := testutil.OpenTestGORM(pool)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)

	// The api-mode world: the push (ADR-0353) would have written both
	// in-window clusters' tags into resolved_tags — cluster C's period is
	// outside the month-snapped window, so the push omits it.
	_, err := pool.Exec(context.Background(), `UPDATE org_container_keys SET resolved_tags = '{"environment":"production"}'::jsonb
		WHERE namespace IN ($1, $2)`, dbTagNSA, dbTagNSB)
	require.NoError(t, err)

	filters := []model.TagFilter{{Key: "environment", Values: []string{"production"}}}
	dbPairs := dbTagRunKeysFilter(t, gdb, "db", filters)
	require.Len(t, dbPairs, 2,
		"the db-mode pairs must be non-empty — an empty-vs-empty agreement would pass the ElementsMatch vacuously (#815 review)")
	apiPairs := dbTagRunKeysFilter(t, gdb, "api", filters)

	assert.ElementsMatch(t, dbPairs, apiPairs,
		"db mode (label summaries) and api mode (pushed resolved_tags) must select the same cluster set for the same org data (#815 close criterion)")
}

// TestTagFilterExistsClause_DBMode_Executed is executed coverage for the
// raw-$N clause builder (the nodes/mig/quota handler path — the gorm sites
// above do not exercise it). It pins BOTH the placeholder numbering and the
// next-free-position convention the callers' argIdx arithmetic continues
// from: an off-by-one in either silently mis-binds every parameter after
// the clause (found live during #815's development).
func TestTagFilterExistsClause_DBMode_Executed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	filters := []model.TagFilter{{Key: "environment", Values: []string{"production"}}}
	clause, args, next := model.TagFilterExistsClause(testutil.TestOrgID, "o.cluster_uuid", "o.namespace", filters, 1)
	require.NotEmpty(t, clause)
	assert.Equal(t, len(args)+1, next,
		"the clause must return the NEXT FREE position (len(args)+1 from base 1) — the convention the callers' argIdx arithmetic expects")

	sql := `SELECT o.cluster_uuid::text, o.namespace FROM org_container_keys o WHERE o.org_id = '` + testutil.TestOrgID + `' AND ` + clause
	rows, err := pool.Query(context.Background(), sql, args...)
	require.NoError(t, err)
	defer func() { rows.Close() }()

	var got [][2]string
	for rows.Next() {
		var cluster, ns string
		require.NoError(t, rows.Scan(&cluster, &ns))
		got = append(got, [2]string{cluster, ns})
	}
	assert.ElementsMatch(t, [][2]string{{dbTagClusterA, dbTagNSA}, {dbTagClusterB, dbTagNSB}}, got,
		"the raw clause must match both in-window clusters and exclude the out-of-window one")
}

// TestTagFilterExistsClauseForCommaSeparatedNamespaces_DBMode_Executed is
// executed coverage for the comma-separated-namespaces clause builder (the
// PVC/cluster-quota handler path), pinning the same conventions.
func TestTagFilterExistsClauseForCommaSeparatedNamespaces_DBMode_Executed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagTwoClusterWorld(t, pool, schema)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	filters := []model.TagFilter{{Key: "environment", Values: []string{"production"}}}

	for _, tc := range []struct {
		name       string
		cluster    string
		namespaces string
		want       bool
	}{
		{"in-window cluster and namespace", dbTagClusterA, dbTagNSA, true},
		{"in-window cluster, other in-window namespace", dbTagClusterB, dbTagNSB, true},
		{"out-of-window cluster", dbTagClusterC, dbTagNSC, false},
		{"in-window cluster, unknown namespace", dbTagClusterA, "ns-unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clause, args, next := model.TagFilterExistsClauseForCommaSeparatedNamespaces(
				testutil.TestOrgID, "'"+tc.cluster+"'", "'"+tc.namespaces+"'", filters, 1)
			require.NotEmpty(t, clause)
			assert.Equal(t, len(args)+1, next, "the next-free-position convention")

			var matched bool
			err := pool.QueryRow(context.Background(), `SELECT `+clause, args...).Scan(&matched)
			require.NoError(t, err)
			assert.Equal(t, tc.want, matched)
		})
	}
}

// seedDBTagCrossClusterWorld builds the shape #867 leaves behind: the
// reporting_ocptags_values arrays rebuilt over the whole reporting window,
// with cluster_ids and namespaces aggregated independently. Positional
// unnest() then pairs each cluster with a namespace it never carried the tag
// in. The label summaries keep the truth — cluster A tags ns-a only, cluster
// B tags ns-b only.
func seedDBTagCrossClusterWorld(t *testing.T, pool *pgxpool.Pool, schema string) {
	t.Helper()
	ctx := context.Background()
	_, thisMonthStart := dbTagMonthStarts()

	_, err := pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_enabledtagkeys (uuid, key, enabled, provider_type)
		VALUES (gen_random_uuid(), 'environment', true, 'OCP') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_ocpusagereportperiod (id, cluster_id, report_period_start)
		VALUES (1, $1, $3), (2, $2, $3)`, dbTagClusterA, dbTagClusterB, thisMonthStart)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_ocpusagepodlabel_summary (uuid, key, values, report_period_id, namespace, node)
		VALUES
			(gen_random_uuid(), 'environment', ARRAY['production'], 1, $1, 'node-a'),
			(gen_random_uuid(), 'environment', ARRAY['production'], 2, $2, 'node-b')`,
		dbTagNSA, dbTagNSB)
	require.NoError(t, err)

	// Both clusters, both namespaces — arrays in independent orders, so
	// unnest(tv.cluster_ids, tv.namespaces) reads (A, ns-b) and (B, ns-a).
	_, err = pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_ocptags_values (uuid, key, value, cluster_ids, namespaces)
		VALUES (gen_random_uuid(), 'environment', 'production', ARRAY[$1, $2], ARRAY[$4, $3])`,
		dbTagClusterA, dbTagClusterB, dbTagNSA, dbTagNSB)
	require.NoError(t, err)
}

// TestMatchingContainerKeys_DBMode_NoPositionalArrayPairing is executed
// coverage for the raw pgx container-key site (node GPU recommendations).
// Once #867 rebuilds the catalog arrays over the whole window, unnest() of
// cluster_ids and namespaces pairs them by position — here that read is
// (A, ns-b) and (B, ns-a), neither of which carries the tag. The filter must
// match only the label summaries' true pairs, and the keys table rows for the
// false pairs must not come back.
func TestMatchingContainerKeys_DBMode_NoPositionalArrayPairing(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagCrossClusterWorld(t, pool, schema)

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
		VALUES
			($5, $1, $3, 'w-a', 'Deployment', 'c-a'),
			($5, $1, $4, 'w-b', 'Deployment', 'c-b'),
			($5, $2, $3, 'w-c', 'Deployment', 'c-c'),
			($5, $2, $4, 'w-d', 'Deployment', 'c-d')
		ON CONFLICT DO NOTHING`,
		dbTagClusterA, dbTagClusterB, dbTagNSA, dbTagNSB, testutil.TestOrgID)
	require.NoError(t, err)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	keys, err := model.MatchingContainerKeys(ctx, pool, testutil.TestOrgID,
		[]model.TagFilter{{Key: "environment", Values: []string{"production"}}})
	require.NoError(t, err)
	require.NotNil(t, keys, "the filter must produce a key set, not match everything")

	assert.True(t, keys.Contains(dbTagClusterA, dbTagNSA, "w-a", "c-a"),
		"cluster A carries the tag in its label summary for ns-a")
	assert.True(t, keys.Contains(dbTagClusterB, dbTagNSB, "w-d", "c-d"),
		"cluster B carries the tag in its label summary for ns-b")
	assert.False(t, keys.Contains(dbTagClusterA, dbTagNSB, "w-b", "c-b"),
		"positional unnest() pairs cluster A with ns-b, which its label summary never tags")
	assert.False(t, keys.Contains(dbTagClusterB, dbTagNSA, "w-c", "c-c"),
		"positional unnest() pairs cluster B with ns-a, which its label summary never tags")
	assert.Len(t, keys, 2, "exactly the two true (cluster, namespace) pairs must match")
}

// TestApplyTagFiltersToNSKeys_DBMode_NoPositionalArrayPairing is the same
// executed check for the gorm namespace-key site (namespace recommendation
// lists), built on org_namespace_keys.
func TestApplyTagFiltersToNSKeys_DBMode_NoPositionalArrayPairing(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	gdb := testutil.OpenTestGORM(pool)
	schema := setupKokuLabelSchema(t, pool)
	seedDBTagCrossClusterWorld(t, pool, schema)

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO org_namespace_keys (org_id, cluster_uuid, namespace_name)
		VALUES ($5, $1, $3), ($5, $1, $4), ($5, $2, $3), ($5, $2, $4)
		ON CONFLICT DO NOTHING`,
		dbTagClusterA, dbTagClusterB, dbTagNSA, dbTagNSB, testutil.TestOrgID)
	require.NoError(t, err)

	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	query := gdb.Table("org_namespace_keys onk").
		Select("onk.cluster_uuid::text, onk.namespace_name").
		Where("onk.org_id = ?", testutil.TestOrgID)
	query = model.ApplyTagFiltersToNSKeys(query, testutil.TestOrgID,
		[]model.TagFilter{{Key: "environment", Values: []string{"production"}}})

	rows, err := query.Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var pairs [][2]string
	for rows.Next() {
		var cluster, ns string
		require.NoError(t, rows.Scan(&cluster, &ns))
		pairs = append(pairs, [2]string{cluster, ns})
	}
	require.NoError(t, rows.Err())

	assert.ElementsMatch(t, [][2]string{{dbTagClusterA, dbTagNSA}, {dbTagClusterB, dbTagNSB}}, pairs,
		"only the label summaries' true pairs may match; unnest() would also return (A, ns-b) and (B, ns-a)")
}
