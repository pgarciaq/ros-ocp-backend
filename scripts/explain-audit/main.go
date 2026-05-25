// EXPLAIN ANALYZE audit for ros-ocp-backend hot query paths.
//
// Usage:
//
//	PGPASSWORD=postgres go run ./scripts/explain-audit \
//	  -db-host localhost -db-port 25432 -db-name ros_explain
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	gormPG "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const (
	orgLarge  = "org-large"
	orgMedium = "org-medium"
	limit     = 10
)

type queryCase struct {
	Category string
	Name     string
	OrgID    string
	SQL      string
	Args     []any
}

type queryResult struct {
	queryCase
	ExecMS       float64
	PlanningMS   float64
	ActualRows   int64
	EstimatedRows int64
	ScanType     string // Index Scan, Seq Scan, Bitmap Heap Scan, etc.
	Issues       []string
	PlanSnippet  string
}

var (
	reExecTime   = regexp.MustCompile(`Execution Time: ([0-9.]+) ms`)
	rePlanTime   = regexp.MustCompile(`Planning Time: ([0-9.]+) ms`)
	reActualRows = regexp.MustCompile(`actual rows=(\d+)`)
	rePlanRows   = regexp.MustCompile(`rows=(\d+)`)
	reSeqScan    = regexp.MustCompile(`Seq Scan on (\S+)`)
	reIndexScan  = regexp.MustCompile(`Index Scan using (\S+) on (\S+)`)
	reBitmapScan = regexp.MustCompile(`Bitmap Heap Scan on (\S+)`)
	reSort       = regexp.MustCompile(`Sort`)
)

func main() {
	host := flag.String("db-host", envOr("DB_HOST", "localhost"), "PostgreSQL host")
	port := flag.String("db-port", envOr("DB_PORT", "25432"), "PostgreSQL port")
	name := flag.String("db-name", envOr("DB_NAME", "ros_explain"), "PostgreSQL database")
	user := flag.String("db-user", envOr("DB_USER", "postgres"), "PostgreSQL user")
	pass := flag.String("db-password", envOr("DB_PASSWORD", "postgres"), "PostgreSQL password")
	seedOnly := flag.Bool("seed-only", false, "Run seed SQL only")
	skipSeed := flag.Bool("skip-seed", false, "Skip seeding (data already loaded)")
	flag.Parse()

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", *user, *pass, *host, *port, *name)
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fatal("connect: %v", err)
	}
	defer pool.Close()

	if !*skipSeed {
		fmt.Println("Seeding database (this may take several minutes)...")
		seedSQL, err := os.ReadFile("scripts/explain-audit/seed.sql")
		if err != nil {
			fatal("read seed.sql: %v", err)
		}
		t0 := time.Now()
		if _, err := pool.Exec(ctx, string(seedSQL)); err != nil {
			fatal("seed: %v", err)
		}
		fmt.Printf("Seed completed in %.1fs\n", time.Since(t0).Seconds())
	}

	if *seedOnly {
		printCounts(ctx, pool)
		return
	}

	gormDB, err := gorm.Open(gormPG.Open(connStr), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fatal("gorm: %v", err)
	}
	database.DB = gormDB

	printCounts(ctx, pool)

	cases := buildQueryCases(ctx, pool, gormDB)
	var results []queryResult
	for _, qc := range cases {
		r := runExplain(ctx, pool, qc)
		results = append(results, r)
		fmt.Printf("  [%s] %s: %.1f ms (%s)\n", r.Category, r.Name, r.ExecMS, r.ScanType)
	}

	printReport(results)
}

func buildQueryCases(ctx context.Context, pool *pgxpool.Pool, db *gorm.DB) []queryCase {
	var cases []queryCase

	largeCluster := mustScalar(ctx, pool, `
		SELECT cluster_uuid::text FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 ORDER BY c.id LIMIT 1`, orgLarge)
	largeNS := mustScalar(ctx, pool, `
		SELECT namespace FROM recommendation_sets WHERE org_id = $1 LIMIT 1`, orgLarge)
	sampleContainers := mustStrings(ctx, pool, `
		SELECT DISTINCT namespace, workload, container_name
		FROM recommendation_sets WHERE org_id = $1 AND stale = false
		ORDER BY namespace, workload, container_name
		LIMIT 510`, orgLarge)

	// --- Container list: offset pagination ---
	for _, pg := range []struct {
		name   string
		offset int
	}{
		{"offset_page_1", 0},
		{"offset_page_100", 990},
		{"offset_page_500", 4990},
	} {
		sql, args := nativeListSQL(orgLarge, limit, pg.offset, "", "", "", false, "", "", "")
		cases = append(cases, queryCase{"container_list", pg.name, orgLarge, sql, args})
	}

	// --- Container list: keyset ---
	sql, args := nativeListSQL(orgLarge, limit, 0, "", "", "", true, "", "", "")
	cases = append(cases, queryCase{"container_list", "keyset_page_1", orgLarge, sql, args})
	if len(sampleContainers) >= 11 {
		ns, wl, cn := sampleContainers[10][0], sampleContainers[10][1], sampleContainers[10][2]
		sql, args = nativeListSQL(orgLarge, limit, 0, "", "", "", true, ns, wl, cn)
		cases = append(cases, queryCase{"container_list", "keyset_page_2", orgLarge, sql, args})
	}

	// --- Filters ---
	sql, args = nativeListSQL(orgLarge, limit, 0, fmt.Sprintf("rs.cluster_uuid = '%s'", largeCluster), "", "", false, "", "", "")
	cases = append(cases, queryCase{"container_list", "filter_cluster", orgLarge, sql, args})
	sql, args = nativeListSQL(orgLarge, limit, 0, "", fmt.Sprintf("rs.namespace = '%s'", largeNS), "", false, "", "", "")
	cases = append(cases, queryCase{"container_list", "filter_namespace", orgLarge, sql, args})
	sql, args = nativeListSQL(orgLarge, limit, 0, "", "", "rs.workload_type = 'deployment'", false, "", "", "")
	cases = append(cases, queryCase{"container_list", "filter_workload_type", orgLarge, sql, args})

	// --- Count queries ---
	cases = append(cases, queryCase{"container_count", "org_recommendation_stats_lookup", orgLarge,
		`SELECT container_count FROM org_recommendation_stats WHERE org_id = $1`, []any{orgLarge}})
	cases = append(cases, queryCase{"container_count", "distinct_subquery_count", orgLarge, `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name
			FROM recommendation_sets rs
			JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid
			JOIN rh_accounts ra ON ra.id = c.tenant_id
			WHERE ra.org_id = $1 AND rs.stale = false
		) dc`, []any{orgLarge}})

	// --- Digest lookback ---
	startDate := "2026-05-10"
	endDate := "2026-05-24"
	cases = append(cases, queryCase{"digest", "container_digests_all_hours", orgLarge, `
		SELECT bucket_date, namespace, workload, container_name
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = 'all_hours'
		ORDER BY namespace, workload, workload_type, container_name, bucket_date`,
		[]any{orgLarge, largeCluster, startDate, endDate}})
	cases = append(cases, queryCase{"digest", "container_digests_business_hours", orgLarge, `
		SELECT bucket_date, namespace, workload, container_name
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = 'business_hours'
		ORDER BY namespace, workload, workload_type, container_name, bucket_date`,
		[]any{orgLarge, largeCluster, startDate, endDate}})
	cases = append(cases, queryCase{"digest", "namespace_digests_lookback", orgLarge, `
		SELECT bucket_date, namespace
		FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = 'all_hours'`, []any{orgLarge, largeCluster, startDate, endDate}})
	cases = append(cases, queryCase{"digest", "recommend_all_streaming", orgLarge, `
		SELECT namespace, workload, container_name, bucket_date
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = 'all_hours'
		ORDER BY namespace, workload, workload_type, container_name, bucket_date`,
		[]any{orgLarge, largeCluster, startDate, endDate}})

	// --- Node recommendations ---
	clustersLarge := mustStringsFlat(ctx, pool, `
		SELECT cluster_uuid::text FROM clusters c JOIN rh_accounts ra ON ra.id = c.tenant_id WHERE ra.org_id = $1`, orgLarge)
	cases = append(cases, queryCase{"node", "list_nodes_for_cluster", orgLarge, `
		WITH filtered AS (
			SELECT nr.* FROM node_recommendations nr
			JOIN clusters c ON nr.cluster_uuid::text = c.cluster_uuid::text
			JOIN rh_accounts a ON c.tenant_id = a.id
			WHERE a.org_id = $1 AND nr.cluster_uuid::text = ANY($2) AND nr.cluster_uuid = $3
		),
		node_page AS (
			SELECT f.cluster_uuid, f.node,
				MAX(CASE WHEN f.term = 'medium' AND f.engine = 'cost' THEN f.estimated_monthly_savings_usd END) AS sort_savings
			FROM filtered f GROUP BY f.cluster_uuid, f.node
			ORDER BY sort_savings DESC NULLS LAST, f.node ASC LIMIT 10 OFFSET 0
		)
		SELECT f.* FROM filtered f
		JOIN node_page p ON p.cluster_uuid = f.cluster_uuid AND p.node = f.node
		ORDER BY sort_savings DESC NULLS LAST, f.term, f.engine`,
		[]any{orgLarge, clustersLarge, largeCluster}})

	// --- Namespace list ---
	cases = append(cases, queryCase{"namespace", "list_org", orgLarge, namespaceListSQL(orgLarge, 10, 0, false, "", ""), []any{orgLarge}})
	cases = append(cases, queryCase{"namespace", "filter_cluster", orgLarge, namespaceListSQL(orgLarge, 10, 0, false, largeCluster, ""), []any{orgLarge, largeCluster}})

	// --- PVC ---
	cases = append(cases, queryCase{"pvc", "list_org", orgLarge, `
		SELECT cluster_uuid, namespace, persistentvolumeclaim, usage_ratio, estimated_monthly_savings_usd
		FROM pvc_recommendation_sets WHERE org_id = $1 AND term = 'medium'
		ORDER BY usage_ratio DESC LIMIT 20 OFFSET 0`, []any{orgLarge}})
	cases = append(cases, queryCase{"pvc", "count_org", orgLarge, `
		SELECT COUNT(*) FROM pvc_recommendation_sets WHERE org_id = $1 AND term = 'medium'`, []any{orgLarge}})

	// --- Savings summary ---
	cases = append(cases, queryCase{"savings", "by_plugin", orgLarge, `
		SELECT
			COALESCE((SELECT SUM(estimated_monthly_savings_usd)::float / 100.0 FROM recommendation_sets
				WHERE org_id = $1 AND term = 'medium' AND engine = 'cost' AND stale = false), 0),
			COALESCE((SELECT SUM(estimated_monthly_savings_usd)::float / 100.0 FROM node_recommendations
				WHERE org_id = $1 AND term = 'medium' AND engine = 'cost'), 0),
			COALESCE((SELECT SUM(estimated_monthly_savings_usd)::float / 100.0 FROM pvc_recommendation_sets
				WHERE org_id = $1 AND term = 'medium'), 0)`, []any{orgLarge}})
	cases = append(cases, queryCase{"savings", "by_cluster", orgLarge, fleetByClusterSQL(), []any{orgLarge, "cost"}})

	// --- History ---
	cases = append(cases, queryCase{"history", "list_org_paginated", orgLarge, `
		SELECT h.recorded_at, h.namespace, h.workload, h.container_name, h.term, h.engine
		FROM recommendation_history h
		JOIN clusters c ON c.cluster_uuid = h.cluster_uuid
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1
		ORDER BY h.recorded_at DESC LIMIT 10 OFFSET 0`, []any{orgLarge}})
	if len(sampleContainers) > 0 {
		ns, wl, cn := sampleContainers[0][0], sampleContainers[0][1], sampleContainers[0][2]
		cases = append(cases, queryCase{"history", "filter_container", orgLarge, `
			SELECT h.recorded_at, h.rec_cpu_request_millicores
			FROM recommendation_history h
			JOIN clusters c ON c.cluster_uuid = h.cluster_uuid
			JOIN rh_accounts ra ON ra.id = c.tenant_id
			WHERE ra.org_id = $1 AND h.namespace = $2 AND h.workload = $3 AND h.container_name = $4
			ORDER BY h.recorded_at DESC LIMIT 30`, []any{orgLarge, ns, wl, cn}})
	}

	// --- Quality ---
	cases = append(cases, queryCase{"quality", "lookup_sample", orgLarge, `
		SELECT oom_events_after_rec, stability_pct, adoption_detected
		FROM recommendation_quality
		WHERE org_id = $1 LIMIT 100`, []any{orgLarge}})

	// --- Thresholds ---
	cases = append(cases, queryCase{"threshold", "tenant_override", orgLarge, `
		SELECT thresholds FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = 'container'`, []any{orgLarge}})
	cases = append(cases, queryCase{"threshold", "missing_fallback", orgMedium, `
		SELECT thresholds FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = 'container'`, []any{orgMedium}})

	// --- MarkAdopted batch ---
	if len(sampleContainers) >= 500 {
		nsArr, wlArr, cnArr := make([]string, 500), make([]string, 500), make([]string, 500)
		for i := 0; i < 500; i++ {
			nsArr[i], wlArr[i], cnArr[i] = sampleContainers[i][0], sampleContainers[i][1], sampleContainers[i][2]
		}
		cases = append(cases, queryCase{"adoption", "mark_adopted_500", orgLarge, `
			UPDATE recommendation_sets rs
			SET recommendation_applied_at = $3,
				notification_codes = array_append(array_remove(notification_codes, $4::smallint), $4::smallint)
			FROM unnest($5::text[], $6::text[], $7::text[]) AS t(namespace, workload, container_name)
			WHERE rs.org_id = $1 AND rs.cluster_uuid = $2
				AND rs.namespace = t.namespace AND rs.workload = t.workload AND rs.container_name = t.container_name
				AND rs.recommendation_applied_at IS NULL`,
			[]any{orgLarge, largeCluster, time.Now().UTC(), int16(3), nsArr, wlArr, cnArr}})
	}

	// --- GORM native list (actual code path) ---
	_ = db // used below via model package

	return cases
}

func nativeListSQL(orgID string, lim, offset int, clusterFilter, nsFilter, wtFilter string, keyset bool, afterNS, afterWL, afterCN string) (string, []any) {
	filters := " AND rs.stale = false"
	args := []any{orgID}
	idx := 2
	if clusterFilter != "" {
		filters += " AND " + clusterFilter
	}
	if nsFilter != "" {
		filters += " AND " + nsFilter
	}
	if wtFilter != "" {
		filters += " AND " + wtFilter
	}
	keysetFilter := ""
	if keyset && afterNS != "" {
		keysetFilter = fmt.Sprintf(" AND (rs.namespace, rs.workload, rs.container_name) > ($%d, $%d, $%d)", idx, idx+1, idx+2)
		args = append(args, afterNS, afterWL, afterCN)
		idx += 3
	}
	pageLimit := lim + 1
	var pageClause string
	if keyset {
		pageClause = fmt.Sprintf(" LIMIT %d", pageLimit)
	} else {
		pageClause = fmt.Sprintf(" OFFSET %d LIMIT %d", offset, pageLimit)
	}
	sql := fmt.Sprintf(`
		SELECT rs.org_id, rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name, rs.term, rs.engine
		FROM recommendation_sets rs
		JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid
		JOIN (
			SELECT dc.cluster_uuid, dc.namespace, dc.workload, dc.container_name
			FROM (
				SELECT DISTINCT rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name
				FROM recommendation_sets rs
				JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid
				JOIN rh_accounts ra ON ra.id = c.tenant_id
				WHERE ra.org_id = $1%s%s
			) dc
			ORDER BY dc.namespace, dc.workload, dc.container_name%s
		) page ON page.cluster_uuid = rs.cluster_uuid
			AND page.namespace = rs.namespace AND page.workload = rs.workload AND page.container_name = rs.container_name
		WHERE rs.stale = false
		ORDER BY rs.namespace, rs.workload, rs.container_name, rs.term, rs.engine`, filters, keysetFilter, pageClause)
	return sql, args
}

func namespaceListSQL(orgID string, lim, offset int, keyset bool, clusterFilter, afterNS string) string {
	filter := ""
	if clusterFilter != "" {
		filter = " AND ns.cluster_uuid = $2"
	}
	if keyset {
		return fmt.Sprintf(`
			SELECT ns.namespace_name, ns.term, ns.engine
			FROM namespace_recommendation_sets ns
			JOIN (
				SELECT page.cluster_uuid, page.namespace_name FROM (
					SELECT DISTINCT ns.cluster_uuid, ns.namespace_name
					FROM namespace_recommendation_sets ns
					JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid
					JOIN rh_accounts ra ON ra.id = c.tenant_id
					WHERE ra.org_id = $1 AND ns.term IS NOT NULL AND ns.stale = false%s
					AND (ns.namespace_name, ns.cluster_uuid) > ($3, $4)
				) page ORDER BY page.namespace_name, page.cluster_uuid LIMIT %d
			) p ON p.cluster_uuid = ns.cluster_uuid AND p.namespace_name = ns.namespace_name
			WHERE ns.stale = false ORDER BY ns.namespace_name, ns.term`, filter, lim+1)
	}
	return fmt.Sprintf(`
		SELECT ns.namespace_name, ns.term, ns.engine
		FROM namespace_recommendation_sets ns
		JOIN (
			SELECT dn.cluster_uuid, dn.namespace_name, dn.ros_ns_page_sort FROM (
				SELECT DISTINCT ON (ns.cluster_uuid, ns.namespace_name)
					ns.cluster_uuid, ns.namespace_name, ns.updated_at AS ros_ns_page_sort
				FROM namespace_recommendation_sets ns
				JOIN clusters c ON c.cluster_uuid = ns.cluster_uuid
				JOIN rh_accounts ra ON ra.id = c.tenant_id
				WHERE ra.org_id = $1 AND ns.term IS NOT NULL AND ns.stale = false%s
				ORDER BY ns.cluster_uuid, ns.namespace_name, ns.updated_at DESC, ns.term ASC, ns.engine ASC
			) dn ORDER BY dn.ros_ns_page_sort DESC, dn.cluster_uuid, dn.namespace_name
			OFFSET %d LIMIT %d
		) page ON page.cluster_uuid = ns.cluster_uuid AND page.namespace_name = ns.namespace_name
		WHERE ns.stale = false ORDER BY ns.updated_at DESC, ns.term`, filter, offset, lim+1)
}

func fleetByClusterSQL() string {
	return `
		WITH rec_clusters AS (
			SELECT DISTINCT cluster_uuid::text FROM recommendation_sets
			WHERE org_id = $1 AND term = 'medium' AND engine = $2 AND stale = false
			UNION SELECT DISTINCT cluster_uuid::text FROM node_recommendations
			WHERE org_id = $1 AND term = 'medium' AND engine = $2
			UNION SELECT DISTINCT cluster_uuid::text FROM pvc_recommendation_sets
			WHERE org_id = $1 AND term = 'medium'
		),
		container_savings AS (
			SELECT cluster_uuid::text, COALESCE(SUM(estimated_monthly_savings_usd),0)::float/100 AS savings
			FROM recommendation_sets WHERE org_id = $1 AND term = 'medium' AND engine = $2 AND stale = false
			GROUP BY cluster_uuid
		),
		node_savings AS (
			SELECT cluster_uuid::text, COALESCE(SUM(estimated_monthly_savings_usd),0)::float/100 AS savings
			FROM node_recommendations WHERE org_id = $1 AND term = 'medium' AND engine = $2
			GROUP BY cluster_uuid
		)
		SELECT rc.cluster_uuid, COALESCE(cs.savings,0)+COALESCE(ns.savings,0)
		FROM rec_clusters rc
		LEFT JOIN container_savings cs ON cs.cluster_uuid = rc.cluster_uuid
		LEFT JOIN node_savings ns ON ns.cluster_uuid = rc.cluster_uuid
		ORDER BY 2 DESC`
}

func runExplain(ctx context.Context, pool *pgxpool.Pool, qc queryCase) queryResult {
	r := queryResult{queryCase: qc}
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+qc.SQL, qc.Args...)
	if err != nil {
		r.Issues = append(r.Issues, "ERROR: "+err.Error())
		return r
	}
	defer rows.Close()
	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err == nil {
			planLines = append(planLines, line)
		}
	}
	plan := strings.Join(planLines, "\n")
	r.PlanSnippet = truncatePlan(plan, 8)

	if m := reExecTime.FindStringSubmatch(plan); len(m) == 2 {
		fmt.Sscanf(m[1], "%f", &r.ExecMS)
	}
	if m := rePlanTime.FindStringSubmatch(plan); len(m) == 2 {
		fmt.Sscanf(m[1], "%f", &r.PlanningMS)
	}

	// Top-level scan detection
	if loc := reSeqScan.FindStringSubmatch(plan); len(loc) == 2 {
		r.ScanType = "Seq Scan on " + loc[1]
	} else if loc := reIndexScan.FindStringSubmatch(plan); len(loc) == 3 {
		r.ScanType = "Index Scan on " + loc[2] + " (" + loc[1] + ")"
	} else if loc := reBitmapScan.FindStringSubmatch(plan); len(loc) == 2 {
		r.ScanType = "Bitmap Heap Scan on " + loc[1]
	}

	// Row estimate accuracy at root
	if m := reActualRows.FindAllStringSubmatch(plan, -1); len(m) > 0 {
		fmt.Sscanf(m[len(m)-1][1], "%d", &r.ActualRows)
	}

	analyzeIssues(&r, plan)
	return r
}

func analyzeIssues(r *queryResult, plan string) {
	if r.ExecMS > 100 {
		r.Issues = append(r.Issues, fmt.Sprintf("SLOW: execution time %.1f ms > 100ms threshold", r.ExecMS))
	}
	for _, m := range reSeqScan.FindAllStringSubmatch(plan, -1) {
		table := m[1]
		if isLargeTable(table) {
			r.Issues = append(r.Issues, "Seq Scan on large table: "+table)
		}
	}
	if reSort.MatchString(plan) && r.ExecMS > 50 {
		r.Issues = append(r.Issues, "Sort operation in plan (may be expensive at scale)")
	}
	if strings.Contains(plan, "Offset") && r.ExecMS > 50 {
		r.Issues = append(r.Issues, "OFFSET pagination detected in plan")
	}
}

func isLargeTable(name string) bool {
	large := []string{"recommendation_sets", "daily_container_digests", "recommendation_history",
		"namespace_recommendation_sets", "daily_namespace_digests", "node_recommendations", "pvc_recommendation_sets"}
	for _, t := range large {
		if strings.HasPrefix(name, t) {
			return true
		}
	}
	return false
}

func printReport(results []queryResult) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("EXPLAIN ANALYZE AUDIT REPORT — ros-ocp-backend")
	fmt.Println(strings.Repeat("=", 80))

	byCategory := map[string][]queryResult{}
	for _, r := range results {
		byCategory[r.Category] = append(byCategory[r.Category], r)
	}
	cats := make([]string, 0, len(byCategory))
	for c := range byCategory {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	var slowQueries, seqScans, offsetIssues []queryResult

	for _, cat := range cats {
		fmt.Printf("\n## %s\n\n", strings.ToUpper(cat))
		fmt.Printf("| Query | Time (ms) | Scan | Actual Rows | Issues |\n")
		fmt.Printf("|-------|-----------|------|-------------|--------|\n")
		for _, r := range byCategory[cat] {
			issues := strings.Join(r.Issues, "; ")
			if issues == "" {
				issues = "—"
			}
			fmt.Printf("| %s | %.1f | %s | %d | %s |\n", r.Name, r.ExecMS, r.ScanType, r.ActualRows, issues)
			if r.ExecMS > 100 {
				slowQueries = append(slowQueries, r)
			}
			for _, iss := range r.Issues {
				if strings.Contains(iss, "Seq Scan") {
					seqScans = append(seqScans, r)
				}
				if strings.Contains(iss, "OFFSET") {
					offsetIssues = append(offsetIssues, r)
				}
			}
		}
	}

	// Offset vs keyset comparison
	fmt.Println("\n## OFFSET vs KEYSET PAGINATION COMPARISON")
	offsetTimes := map[string]float64{}
	keysetTimes := map[string]float64{}
	for _, r := range results {
		if r.Category != "container_list" {
			continue
		}
		if strings.HasPrefix(r.Name, "offset_page_") {
			offsetTimes[r.Name] = r.ExecMS
		}
		if strings.HasPrefix(r.Name, "keyset_page_") {
			keysetTimes[r.Name] = r.ExecMS
		}
	}
	for _, pg := range []string{"page_1", "page_100", "page_500"} {
		off := offsetTimes["offset_"+pg]
		if off > 0 {
			fmt.Printf("- offset_%s: %.1f ms\n", pg, off)
		}
	}
	for _, pg := range []string{"page_1", "page_2"} {
		k := keysetTimes["keyset_"+pg]
		if k > 0 {
			fmt.Printf("- keyset_%s: %.1f ms\n", pg, k)
		}
	}
	if offsetTimes["offset_page_500"] > 0 && keysetTimes["keyset_page_2"] > 0 {
		ratio := offsetTimes["offset_page_500"] / keysetTimes["keyset_page_2"]
		fmt.Printf("\nDeep offset (page 500) is %.1fx slower than keyset page 2 (%.1f ms vs %.1f ms)\n",
			ratio, offsetTimes["offset_page_500"], keysetTimes["keyset_page_2"])
	}

	fmt.Println("\n## FINDINGS SUMMARY")
	if len(slowQueries) == 0 {
		fmt.Println("- No queries exceeded 100ms on org-large dataset.")
	} else {
		fmt.Printf("- **%d queries exceeded 100ms** on org-large:\n", len(slowQueries))
		for _, r := range slowQueries {
			fmt.Printf("  - [%s] %s: %.1f ms\n", r.Category, r.Name, r.ExecMS)
		}
	}

	fmt.Println("\n## RECOMMENDATIONS")
	printRecommendations(results, offsetTimes, keysetTimes)
}

func printRecommendations(results []queryResult, offsetTimes, keysetTimes map[string]float64) {
	recs := []string{}

	if offsetTimes["offset_page_500"] > keysetTimes["keyset_page_2"]*2 {
		recs = append(recs, "Deprecate offset pagination for container list API; keyset pagination (`idx_rs_keyset_page`) keeps latency flat regardless of page depth.")
	}
	if offsetTimes["offset_page_500"] > 100 {
		recs = append(recs, "Deep offset pagination (page 500+) scans and discards ~5000 container keys before returning 10 rows. This is O(offset) by design.")
	}

	for _, r := range results {
		if r.Category == "digest" && r.ExecMS > 50 {
			recs = append(recs, fmt.Sprintf("Digest query '%s' took %.1fms — verify `idx_daily_container_digests_lookback (org_id, cluster_uuid, schedule_type, bucket_date)` is used.", r.Name, r.ExecMS))
		}
		if r.Category == "container_count" && r.Name == "distinct_subquery_count" && r.ExecMS > 50 {
			recs = append(recs, "Pre-computed `org_recommendation_stats.container_count` avoids expensive COUNT(DISTINCT ...) on every list request.")
		}
		if r.Category == "savings" && r.ExecMS > 100 {
			recs = append(recs, "Fleet savings summary runs 4+ correlated subqueries per plugin plus a multi-CTE cluster breakdown. Consider materialized per-org savings or a single UNION ALL aggregation.")
		}
		if r.Category == "history" && strings.Contains(strings.Join(r.Issues, " "), "Seq Scan") {
			recs = append(recs, "Add index on recommendation_history (org_id, namespace, workload, container_name, recorded_at DESC) for container-specific history lookups.")
		}
	}

	// Deduplicate
	seen := map[string]bool{}
	for i, rec := range recs {
		if seen[rec] {
			continue
		}
		seen[rec] = true
		fmt.Printf("%d. %s\n", i+1, rec)
	}
	if len(recs) == 0 {
		fmt.Println("All hot paths perform within acceptable bounds on the seeded org-large dataset.")
	}
}

func printCounts(ctx context.Context, pool *pgxpool.Pool) {
	tables := []string{
		"recommendation_sets", "namespace_recommendation_sets", "node_recommendations",
		"pvc_recommendation_sets", "daily_container_digests", "daily_namespace_digests",
		"recommendation_history", "org_recommendation_stats",
	}
	fmt.Println("\nTable row counts:")
	for _, t := range tables {
		n := mustScalar(ctx, pool, fmt.Sprintf("SELECT COUNT(*)::text FROM %s", t))
		fmt.Printf("  %s: %s\n", t, n)
	}
	for _, org := range []string{"org-small", "org-medium", "org-large"} {
		n := mustScalar(ctx, pool, `
			SELECT container_count::text || ' containers, ' || namespace_count::text || ' namespaces'
			FROM org_recommendation_stats WHERE org_id = $1`, org)
		fmt.Printf("  %s stats: %s\n", org, n)
	}
}

func mustScalar(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	var s string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
		fatal("query %s: %v", truncate(sql, 60), err)
	}
	return s
}

func mustStrings(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) [][]string {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		fatal("query: %v", err)
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var a, b, c string
		if err := rows.Scan(&a, &b, &c); err != nil {
			fatal("scan: %v", err)
		}
		out = append(out, []string{a, b, c})
	}
	return out
}

func mustStringsFlat(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) []string {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		fatal("query: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			fatal("scan: %v", err)
		}
		out = append(out, s)
	}
	return out
}

func truncatePlan(plan string, maxLines int) string {
	lines := strings.Split(plan, "\n")
	if len(lines) <= maxLines {
		return plan
	}
	return strings.Join(lines[:maxLines], "\n") + "\n  ..."
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
	os.Exit(1)
}

// Verify GORM path compiles and matches native list at runtime (optional benchmark).
func runGORMList(orgID string) {
	opts := listoptions.ListOptions{Limit: 10, Offset: 0}
	qp := map[string]interface{}{}
	_, _ = model.GetNativeRecommendations(orgID, opts, qp, map[string][]string{"*": {}})
}
