package model_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// BenchmarkTagFilterKeys_DBMode measures the per-query cost of the db-mode
// tag filter over a pessimistically large org (#815's owner ask): the
// filtered key is present on EVERY namespace of every cluster, two nodes
// per namespace, both window periods, both label sources — plus background
// rows from other enabled keys so the tables are realistically sized. The
// 200-enabled-key cap is respected (31 keys seeded).
//
// The "old-source" sub-benchmark inlines the pre-#815 predicate (the
// reporting_ocptags_values unnest) over the same org data for an
// apples-to-apples comparison; the "label-summaries" sub-benchmark runs the
// shipped ApplyTagFiltersToKeys.
func BenchmarkTagFilterKeys_DBMode(b *testing.B) {
	const (
		namespacesPerCluster = 150
		clusters             = 2
		backgroundKeys       = 30
	)
	pool := testutil.SetupTestDB(b)
	gdb := testutil.OpenTestGORM(pool)
	schema := seedBenchmarkTagWorld(b, pool, namespacesPerCluster, clusters, backgroundKeys)

	config.ResetTagsForTest()
	b.Setenv("ROS_TAGS_ENABLED", "true")
	b.Setenv("ROS_TAGS_SOURCE", "db")

	filters := []model.TagFilter{{Key: "environment", Values: []string{"production"}}}

	oldPredicate := fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM %s tv,
		     unnest(tv.cluster_ids, tv.namespaces) AS t(cluster_id, namespace)
		WHERE tv.key = ?
		  AND tv.value IN ?
		  AND t.cluster_id = ock.cluster_uuid::text
		  AND t.namespace = ock.namespace
	)`, pgxIdentifier(schema, "reporting_ocptags_values"))

	b.Run("old-source-tags-values", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			query := gdb.Table("org_container_keys ock").
				Select("ock.cluster_uuid::text, ock.namespace").
				Where("ock.org_id = ?", testutil.TestOrgID).
				Where(oldPredicate, "environment", []string{"production"})
			rows, err := query.Rows()
			if err != nil {
				b.Fatal(err)
			}
			count := 0
			for rows.Next() {
				count++
			}
			_ = rows.Close()
			if count != namespacesPerCluster*clusters {
				b.Fatalf("expected %d matches, got %d", namespacesPerCluster*clusters, count)
			}
		}
	})

	b.Run("label-summaries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			query := gdb.Table("org_container_keys ock").
				Select("ock.cluster_uuid::text, ock.namespace").
				Where("ock.org_id = ?", testutil.TestOrgID)
			query = model.ApplyTagFiltersToKeys(query, testutil.TestOrgID, filters)
			rows, err := query.Rows()
			if err != nil {
				b.Fatal(err)
			}
			count := 0
			for rows.Next() {
				count++
			}
			_ = rows.Close()
			if count != namespacesPerCluster*clusters {
				b.Fatalf("expected %d matches, got %d", namespacesPerCluster*clusters, count)
			}
		}
	})
}

func pgxIdentifier(schema, table string) string {
	return `"` + schema + `"."` + table + `"`
}

// seedBenchmarkTagWorld builds the benchmark fixture: every namespace of
// every cluster carries environment=production (two nodes, both window
// periods, both label sources); background keys add realistic table bulk;
// reporting_ocptags_values holds the same (cluster, namespace) pairs the
// old predicate would need.
func seedBenchmarkTagWorld(tb testing.TB, pool *pgxpool.Pool, namespacesPerCluster, clusters, backgroundKeys int) string {
	tb.Helper()
	ctx := context.Background()
	schema, err := tags.TenantSchema(testutil.TestOrgID)
	if err != nil {
		tb.Fatal(err)
	}
	_, err = pool.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema+`;`)
	if err != nil {
		tb.Fatal(err)
	}
	// The same tables as the correctness fixture.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE `+schema+`.reporting_enabledtagkeys (uuid UUID PRIMARY KEY, key VARCHAR(512) NOT NULL, enabled BOOLEAN NOT NULL DEFAULT true, provider_type VARCHAR(50) NOT NULL, UNIQUE (key, provider_type));
		CREATE TABLE `+schema+`.reporting_ocptags_values (uuid UUID PRIMARY KEY, key TEXT NOT NULL, value TEXT NOT NULL, cluster_ids TEXT[] NOT NULL, cluster_aliases TEXT[] NOT NULL DEFAULT '{}', namespaces TEXT[] NOT NULL, nodes TEXT[], UNIQUE (key, value));
		CREATE TABLE `+schema+`.reporting_ocpusagereportperiod (id BIGINT PRIMARY KEY, cluster_id TEXT NOT NULL, report_period_start DATE NOT NULL);
		CREATE TABLE `+schema+`.reporting_ocpusagepodlabel_summary (uuid UUID PRIMARY KEY, key TEXT NOT NULL, values TEXT[] NOT NULL, report_period_id BIGINT NOT NULL REFERENCES `+schema+`.reporting_ocpusagereportperiod (id), namespace TEXT NOT NULL, node TEXT);
		CREATE TABLE `+schema+`.reporting_ocpstoragevolumelabel_summary (uuid UUID PRIMARY KEY, key TEXT NOT NULL, values TEXT[] NOT NULL, report_period_id BIGINT NOT NULL REFERENCES `+schema+`.reporting_ocpusagereportperiod (id), namespace TEXT NOT NULL, node TEXT);
	`); err != nil {
		tb.Fatal(err)
	}

	lastMonthStart, thisMonthStart := dbTagMonthStarts()

	// Report periods: both window months per cluster (plus index on the summary shape).
	for c := 0; c < clusters; c++ {
		clusterID := fmt.Sprintf("b%06d0-0000-0000-0000-000000000001", c)
		for p, start := range []interface{}{lastMonthStart, thisMonthStart} {
			if _, err := pool.Exec(ctx, `INSERT INTO `+schema+`.reporting_ocpusagereportperiod (id, cluster_id, report_period_start)
				VALUES ($1, $2, $3)`, int64(c*2+p+1), clusterID, start); err != nil {
				tb.Fatal(err)
			}
		}
	}

	// org_container_keys: one container per namespace.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
		SELECT $1, ('b' || LPAD(g::text, 6, '0') || '0-0000-0000-0000-000000000001')::uuid,
		       'bench-ns-' || g || '-' || n, 'w', 'Deployment', 'c'
		FROM generate_series(0, %d) g, generate_series(1, %d) n
		ON CONFLICT DO NOTHING`, clusters-1, namespacesPerCluster), testutil.TestOrgID); err != nil {
		tb.Fatal(err)
	}

	// The label summaries: the filtered key on EVERY namespace (2 nodes ×
	// 2 periods × 2 sources), plus background keys on every 3rd namespace
	// for bulk.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %[1]s.reporting_ocpusagepodlabel_summary (uuid, key, values, report_period_id, namespace, node)
		SELECT gen_random_uuid(),
		       CASE WHEN k = 0 THEN 'environment' ELSE 'background-key-' || k END,
		       ARRAY[CASE WHEN k = 0 THEN 'production' ELSE 'value-' || k END],
		       rp.id,
		       'bench-ns-' || g || '-' || n,
		       'node-' || d
		FROM %[1]s.reporting_ocpusagereportperiod rp,
		     generate_series(0, %[2]d) g,
		     generate_series(1, %[3]d) n,
		     generate_series(1, 2) d,
		     generate_series(0, %[4]d) k
		WHERE k = 0 OR (n %% 3 = 0 AND k > 0)
	`, schema, clusters-1, namespacesPerCluster, backgroundKeys)); err != nil {
		tb.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %[1]s.reporting_ocpstoragevolumelabel_summary (uuid, key, values, report_period_id, namespace, node)
		SELECT gen_random_uuid(), 'environment', ARRAY['production'], rp.id, 'bench-ns-' || g || '-' || n, NULL
		FROM %[1]s.reporting_ocpusagereportperiod rp,
		     generate_series(0, %[2]d) g,
		     generate_series(1, %[3]d) n
	`, schema, clusters-1, namespacesPerCluster)); err != nil {
		tb.Fatal(err)
	}

	// The enabled keys (the 200-cap respected: 1 + backgroundKeys).
	if _, err := pool.Exec(ctx, `
		INSERT INTO `+schema+`.reporting_enabledtagkeys (uuid, key, enabled, provider_type)
		SELECT gen_random_uuid(), 'environment', true, 'OCP'
		UNION ALL
		SELECT gen_random_uuid(), 'background-key-' || k, true, 'OCP' FROM generate_series(1, $1) k`, backgroundKeys); err != nil {
		tb.Fatal(err)
	}

	// The old source: tags_values with every (cluster, namespace) — the
	// world as it looks right after a summarization run that covered both
	// clusters (the best case for the old predicate).
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		-- unnest(cluster_ids, namespaces) is positional (a zip), so the
		-- arrays must be parallel: each namespace paired with its cluster.
		INSERT INTO %[1]s.reporting_ocptags_values (uuid, key, value, cluster_ids, namespaces)
		SELECT gen_random_uuid(), 'environment', 'production',
		       ARRAY(SELECT 'b' || LPAD(g::text, 6, '0') || '0-0000-0000-0000-000000000001' FROM generate_series(0, %[2]d) g, generate_series(1, %[3]d) n),
		       ARRAY(SELECT 'bench-ns-' || g || '-' || n FROM generate_series(0, %[2]d) g, generate_series(1, %[3]d) n)
	`, schema, clusters-1, namespacesPerCluster)); err != nil {
		tb.Fatal(err)
	}
	return schema
}
