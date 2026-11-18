package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

var validSQLIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func init() {
	for _, dt := range dateRetainedTables {
		if !validSQLIdentifier.MatchString(dt.Table) {
			panic(fmt.Sprintf("retention: invalid table name %q", dt.Table))
		}
		if !validSQLIdentifier.MatchString(dt.DateColumn) {
			panic(fmt.Sprintf("retention: invalid column name %q in table %s", dt.DateColumn, dt.Table))
		}
	}
}

var retentionDropped = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_retention_partitions_dropped_total",
	Help: "Number of partitions dropped by retention sweep",
})

// retentionMarkedStale counts recommendation rows the liveness sweep marked
// stale (#874). It is separate from retentionDropped: marking is reversible and
// deletes nothing, so the two must not share a series.
var retentionMarkedStale = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_retention_rows_marked_stale_total",
	Help: "Number of recommendation rows marked stale because their cluster stopped reporting",
})

// Tables retained by the general ROS_RETENTION_MONTHS setting.
// This fallback list is used when no RetentionProvider plugins are registered.
// When plugins ARE registered, each plugin sweeps its own digest tables via SweepRetention.
var retainedTables = []string{
	"daily_container_digests",
	"daily_namespace_digests",
	"daily_node_digests",
	"gpu_container_digests",
}

// Tables retained by the separate ROS_HISTORY_RETENTION_DAYS setting
// (history/quality grow faster: one row per container×term×engine per run).
// Plugin-owned rows below are ALSO declared via HistoryRetentionProvider
// (#701); each stays here as the plugin-less-binary fallback (overlap
// harmless — DROP IF EXISTS; the documented digest-class pattern).
var historyRetainedTables = []string{
	"recommendation_history",
	"recommendation_quality",
	"pvc_recommendation_quality",
	"vm_recommendation_quality",
	"gpu_mig_recommendation_quality",
	"snapshot_recommendation_quality",
	// #644 SLO rollup store: bounded per-HC buckets + worker pressure,
	// swept with history (90d) rather than digest retention.
	"hosted_api_bucket_rollups",
	"hosted_worker_pressure",
	// #393 thin W5: bounded per-cluster webhook rollups, same 90d sweep.
	// No plugin home (service-level ingest functions own the writes).
	"hosted_api_tax_rollups",
	// #673 NodePool inventory: bounded per-HC pool snapshots, same sweep.
	// No plugin home (service-level ingest functions own the writes).
	"hosted_nodepool_rollups",
	// #392 W4 fleet-capacity snapshots: bounded hourly rows per management
	// cluster, same sweep. Plugin-declared via HistoryRetentionProvider
	// (#701) — swept at the days cutoff by construction.
	"hcp_capacity_history",
}

// RetentionTable is a compile-time-only struct for non-partitioned tables that need
// date-based DELETE retention. Table and DateColumn are sanitized via
// pgx.Identifier.Sanitize() before interpolation into SQL.
type RetentionTable struct {
	Table                string
	DateColumn           string
	InvalidateFleetCache bool
}

var dateRetainedTables = []RetentionTable{
	{"historical_namespace_recommendation_sets", "created_at", false},
	// node_recommendations is not partitioned; plugin partition sweeps are a no-op for it.
	{"node_recommendations", "updated_at", true},
	{"namespace_recommendation_sets", "updated_at", true},
	{"pvc_recommendation_sets", "updated_at", true},
	// #760 sleep evidence: unpartitioned per-container-day legs — date-based
	// DELETE keeps the migration's "bounded" claim true (the sleep window is
	// 14d; the digest-month cutoff is the bound).
	{"hcp_sleep_evidence", "bucket_date", false},
}

// RunRetentionSweep drops monthly partitions older than retentionMonths for
// each retained table. History/quality tables use historyRetentionDays instead
// (default 90 days). Partitions are identified by naming convention
// (<table>_YYYYMM) and compared against the retention cutoff. Also purges
// old rows from non-partitioned tables that use date-based retention.
func RunRetentionSweep(ctx context.Context, pool *pgxpool.Pool, retentionMonths int) error {
	var errs []error
	if retentionMonths <= 0 {
		retentionMonths = 6
	}

	cutoff := time.Now().UTC().AddDate(0, -retentionMonths, 0)
	cutoffYM := cutoff.Format("200601")

	// When RetentionProvider plugins are registered (production binaries import internal/plugins),
	// each plugin sweeps its own partitioned tables via SweepRetention.
	// Tests or tools that omit plugin imports fall back to retainedTables: that slice covers all
	// known partitioned tables (container, namespace, node, GPU) so no table is missed.
	// Plugins still declare those tables when loaded; the overlap is harmless (DROP IF EXISTS).
	//
	// Kruize-only deployments never populate native digest tables (daily_node_digests, gpu_container_digests,
	// daily_pvc_digests, etc.): the legacy path writes workload_metrics / recommendation_sets instead.
	// Sweeping empty tables is a no-op.
	retProviders := plugin.ByTrait[plugin.RetentionProvider]()
	if len(retProviders) > 0 {
		if err := plugin.ExecuteInPhases(ctx, func(ctx context.Context, p plugin.Plugin) error {
			rp, ok := p.(plugin.RetentionProvider)
			if !ok {
				return nil
			}
			if err := rp.SweepRetention(ctx, pool, cutoff); err != nil {
				logging.GetLogger().Warnf("retention: RetentionProvider %s sweep failed: %v", rp.Name(), err)
				return fmt.Errorf("retention plugin %s: %w", rp.Name(), err)
			}
			return nil
		}); err != nil {
			errs = append(errs, err)
		}
	} else {
		if err := SweepPartitionedTables(ctx, pool, retainedTables, cutoffYM); err != nil {
			logging.GetLogger().Warnf("retention: partitioned sweep (main): %v", err)
			errs = append(errs, err)
		}
	}

	cfg := config.GetConfig()

	historyDays := cfg.HistoryRetentionDays
	if historyDays <= 0 {
		historyDays = 90
	}
	historyCutoff := time.Now().UTC().AddDate(0, 0, -historyDays)
	historyCutoffYM := historyCutoff.Format("200601")

	// #701: history-class tables may be plugin-declared via
	// HistoryRetentionProvider — swept HERE at the days cutoff, by
	// construction (never the digest month cutoff). The core
	// historyRetainedTables list is the plugin-less-binary fallback and
	// keeps every entry; plugin declarations ADD to it (overlap harmless,
	// DROP IF EXISTS — the documented digest-class pattern). Collection
	// failure never skips the core fallback sweep.
	historySweepTables := historyRetainedTables
	for _, hp := range plugin.ByTrait[plugin.HistoryRetentionProvider]() {
		for _, table := range hp.HistoryRetentionTables() {
			duplicate := false
			for _, existing := range historySweepTables {
				if existing == table {
					duplicate = true
					break
				}
			}
			if !duplicate {
				historySweepTables = append(historySweepTables, table)
			}
		}
	}
	if err := SweepPartitionedTables(ctx, pool, historySweepTables, historyCutoffYM); err != nil {
		logging.GetLogger().Warnf("retention: partitioned sweep (history): %v", err)
		errs = append(errs, err)
	}

	for _, dt := range dateRetainedTables {
		purged, err := purgeDateRetainedTable(ctx, pool, dt, cutoff)
		if err != nil {
			logging.GetLogger().Warnf("retention: purging %s: %v", dt.Table, err)
			errs = append(errs, fmt.Errorf("purge %s: %w", dt.Table, err))
		} else if purged > 0 {
			retentionDropped.Add(float64(purged))
			logging.GetLogger().Infof("retention: purged %d rows from %s (older than %s)", purged, dt.Table, cutoff.Format("2006-01-02"))
		}
	}

	// Stale recommendation cleanup: delete recommendations that were MARKED stale
	// longer ago than staleCleanupDays (default 30 days). The clock is the mark,
	// not the last report (see purgeStaleRecommendations).
	staleCleanupDays := cfg.StaleCleanupDays
	if staleCleanupDays <= 0 {
		staleCleanupDays = 30
	}
	staleCutoff := time.Now().UTC().AddDate(0, 0, -staleCleanupDays)

	// #874: a cluster that stopped reporting never runs its own cycle, so no
	// per-cycle writer can ever mark it. The liveness sweep is the only actor that
	// notices. It runs before the purge so every row the purge evaluates in this
	// pass has already been through the mark stage, and so a crash between the two
	// phases leaves a reversible mark rather than a half-evaluated purge. A fresh
	// mark is never due in the same sweep: the visible window is 30 days from the
	// mark (marked at day 14, deleted at day 44).
	livenessHours := cfg.ClusterLivenessHours
	if livenessHours <= 0 {
		livenessHours = 336
	}
	livenessCutoff := time.Now().UTC().Add(-time.Duration(livenessHours) * time.Hour)
	markedStale, markErr := markAbandonedClustersStale(ctx, pool, livenessCutoff)
	// Count first, report second: when one table fails the other has still
	// committed, and the metric must describe the work that happened.
	if markedStale > 0 {
		retentionMarkedStale.Add(float64(markedStale))
		logging.GetLogger().Infof("retention: marked %d recommendation rows stale for clusters silent since %s",
			markedStale, livenessCutoff.Format(time.RFC3339))
	}
	if markErr != nil {
		logging.GetLogger().Warnf("retention: marking abandoned clusters stale: %v", markErr)
		errs = append(errs, fmt.Errorf("mark abandoned clusters stale: %w", markErr))
	}
	purgedStale, invalidateErr := purgeStaleRecommendations(ctx, pool, staleCutoff, livenessCutoff)
	if purgedStale > 0 {
		retentionDropped.Add(float64(purgedStale))
		logging.GetLogger().Infof("retention: purged %d stale recommendations (marked before %s)", purgedStale, staleCutoff.Format("2006-01-02"))
	}
	if invalidateErr != nil {
		logging.GetLogger().Warnf("retention: purging stale recommendations: %v", invalidateErr)
		errs = append(errs, fmt.Errorf("purge stale recommendations: %w", invalidateErr))
	}

	// Snapshot inventory retention: purge raw rows older than configured hours (default 48h).
	snapRetentionH := cfg.SnapshotInventoryRetentionH
	if snapRetentionH <= 0 {
		snapRetentionH = 48
	}
	purged, err := ingestion.PurgeSnapshotInventory(ctx, pool, snapRetentionH)
	if err != nil {
		logging.GetLogger().Warnf("retention: %v", err)
		errs = append(errs, fmt.Errorf("purge snapshot inventory: %w", err))
	} else if purged > 0 {
		retentionDropped.Add(float64(purged))
		logging.GetLogger().Infof("retention: purged %d snapshot_inventory rows (older than %dh)", purged, snapRetentionH)
	}
	return errors.Join(errs...)
}

func purgeDateRetainedTable(ctx context.Context, pool *pgxpool.Pool, dt RetentionTable, cutoff time.Time) (int64, error) {
	tableQuoted := pgx.Identifier{dt.Table}.Sanitize()
	colQuoted := pgx.Identifier{dt.DateColumn}.Sanitize()

	if dt.InvalidateFleetCache {
		rows, err := pool.Query(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE %s < $1 RETURNING org_id", tableQuoted, colQuoted),
			cutoff,
		)
		if err != nil {
			return 0, err
		}
		defer rows.Close()

		affectedOrgs := make(map[string]struct{})
		var purged int64
		for rows.Next() {
			var orgID string
			if err := rows.Scan(&orgID); err != nil {
				return purged, err
			}
			purged++
			if orgID != "" {
				affectedOrgs[orgID] = struct{}{}
			}
		}
		if err := rows.Err(); err != nil {
			return purged, err
		}
		for orgID := range affectedOrgs {
			fleetsummary.InvalidateOrg(orgID)
			fleetheatmap.InvalidateOrg(orgID)
			clustercache.InvalidateOrg(orgID)
		}
		return purged, nil
	}

	sql := fmt.Sprintf("DELETE FROM %s WHERE %s < $1", tableQuoted, colQuoted)
	tag, err := pool.Exec(ctx, sql, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func purgeStaleRecommendations(ctx context.Context, pool *pgxpool.Pool, staleCutoff, livenessCutoff time.Time) (int64, error) {
	var (
		purged       int64
		errs         []error
		affectedOrgs = make(map[string]struct{})
	)
	for _, table := range recommendationLivenessTables {
		// #874: the deadline runs from staled_at (when the row was MARKED), so a
		// row stays visible for ROS_STALE_CLEANUP_DAYS after it was hidden, never
		// from its last report. COALESCE covers rows that were already stale
		// before staled_at existed (000223): they keep the deadline they had.
		//
		// The NOT EXISTS guard sits on the irreversible step: a cluster whose
		// clusters row is gone still purges (empty subquery, the Sources destroy
		// path), but a cluster that is in fact reporting survives a mistaken mark.
		query := fmt.Sprintf(`
			DELETE FROM %s rs
			WHERE rs.stale = true
			  AND COALESCE(rs.staled_at, rs.updated_at) < $1
			  AND NOT EXISTS (SELECT 1 FROM clusters c
			                  WHERE c.org_id = rs.org_id AND c.cluster_uuid = rs.cluster_uuid
			                    AND c.last_reported_at >= $2)
			RETURNING rs.org_id`,
			pgx.Identifier{table}.Sanitize())

		rows, err := pool.Query(ctx, query, staleCutoff, livenessCutoff)
		if err != nil {
			errs = append(errs, fmt.Errorf("purge stale %s: %w", table, err))
			continue
		}
		for rows.Next() {
			var orgID string
			if err := rows.Scan(&orgID); err != nil {
				errs = append(errs, fmt.Errorf("scan purged org from %s: %w", table, err))
				break
			}
			purged++
			if orgID != "" {
				affectedOrgs[orgID] = struct{}{}
			}
		}
		if err := rows.Err(); err != nil {
			errs = append(errs, fmt.Errorf("purge stale %s rows: %w", table, err))
		}
		rows.Close()
	}
	for orgID := range affectedOrgs {
		fleetsummary.InvalidateOrg(orgID)
		fleetheatmap.InvalidateOrg(orgID)
		clustercache.InvalidateOrg(orgID)
		// Deleting rows leaves their catalog entries behind; the refresh prunes
		// them, so filter[stale]=only stops counting rows that no longer exist.
		if err := refreshOrgListCatalogs(ctx, pool, orgID); err != nil {
			errs = append(errs, err)
		}
	}
	return purged, errors.Join(errs...)
}

// SweepPartitionedTables drops monthly partitions older than cutoffYM (YYYYMM)
// for each parent table. Each DROP runs inside a short transaction with a 2-second
// lock_timeout so that contention with concurrent reads causes a non-fatal skip
// instead of a lock convoy (DROP TABLE requires AccessExclusiveLock).
func SweepPartitionedTables(ctx context.Context, pool *pgxpool.Pool, tables []string, cutoffYM string) error {
	var errs []error
	for _, table := range tables {
		partitions, err := listPartitions(ctx, pool, table)
		if err != nil {
			logging.GetLogger().Warnf("retention: listing partitions for %s: %v", table, err)
			errs = append(errs, fmt.Errorf("list partitions %s: %w", table, err))
			continue
		}
		for _, part := range partitions {
			ym := extractYearMonth(part, table)
			if ym == "" || ym >= cutoffYM {
				continue
			}
			if err := dropPartitionWithTimeout(ctx, pool, part); err != nil {
				logging.GetLogger().Warnf("retention: dropping %s: %v", part, err)
				errs = append(errs, fmt.Errorf("drop partition %s: %w", part, err))
			} else {
				retentionDropped.Inc()
				logging.GetLogger().Infof("retention: dropped partition %s (older than %s)", part, cutoffYM)
			}
		}
	}
	return errors.Join(errs...)
}

// dropPartitionWithTimeout drops a single partition inside a transaction with
// SET LOCAL lock_timeout = '2s'. If the lock cannot be acquired within 2 seconds
// (e.g. concurrent readers hold AccessShareLock), the error is returned to the
// caller which logs it as non-fatal — the partition will be retried next sweep.
func dropPartitionWithTimeout(ctx context.Context, pool *pgxpool.Pool, partition string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '2s'"); err != nil {
		return fmt.Errorf("set lock_timeout: %w", err)
	}

	quoted := pgx.Identifier{partition}.Sanitize()
	if _, err := tx.Exec(ctx, "DROP TABLE IF EXISTS "+quoted); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// listPartitions returns child partition names for a parent table.
func listPartitions(ctx context.Context, pool *pgxpool.Pool, parentTable string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_inherits i ON i.inhrelid = c.oid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = $1 AND c.relkind = 'r'
		ORDER BY c.relname`, parentTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// extractYearMonth extracts the YYYYMM suffix from a partition name like
// "recommendation_history_202603".
func extractYearMonth(partName, parentTable string) string {
	prefix := parentTable + "_"
	if !strings.HasPrefix(partName, prefix) {
		return ""
	}
	suffix := strings.TrimPrefix(partName, prefix)
	if len(suffix) != 6 {
		return ""
	}
	return suffix
}

// StartRetentionTicker runs RunRetentionSweep immediately and then every 24
// hours. The goroutine exits when ctx is cancelled.
func StartRetentionTicker(ctx context.Context, pool *pgxpool.Pool, retentionMonths int) {
	if err := RunRetentionSweep(ctx, pool, retentionMonths); err != nil {
		logging.GetLogger().Warnf("retention: sweep finished with errors: %v", err)
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := RunRetentionSweep(ctx, pool, retentionMonths); err != nil {
				logging.GetLogger().Warnf("retention: sweep finished with errors: %v", err)
			}
		}
	}
}

// recommendationLivenessTables are the two recommendation tables the liveness
// sweep marks and the stale purge reclaims.
// Both carry cluster_uuid as UUID -- 000032 for the namespace table, and 000041
// for recommendation_sets (which also deletes rows whose text value was not a
// UUID), so the equality to clusters.cluster_uuid needs no cast.
//
// Legacy Kruize namespace rows carry cluster_uuid NULL; the equality never
// matches, so they are never marked by this sweep.
var recommendationLivenessTables = []string{
	"recommendation_sets",
	"namespace_recommendation_sets",
}

// markAbandonedClustersStale marks recommendation rows stale when every
// clusters row for their (org_id, cluster_uuid) reported before livenessCutoff.
//
// The first EXISTS skips a cluster that was registered but never reported
// (last_reported_at IS NULL): it is not abandoned, it is unknown, and marking it
// would make a fresh registration invisible. The second uses NOT EXISTS rather
// than a plain comparison so a cluster with several alias rows stays live while
// any one of them is recent.
//
// Marking is reversible: both upserts clear stale and staled_at when the cluster
// reports again. Nothing is deleted here, so this runs before the purge rather
// than instead of it.
func markAbandonedClustersStale(ctx context.Context, pool *pgxpool.Pool, livenessCutoff time.Time) (int64, error) {
	var (
		marked       int64
		errs         []error
		affectedOrgs = make(map[string]struct{})
	)
	for _, table := range recommendationLivenessTables {
		query := fmt.Sprintf(`
			UPDATE %s rs
			SET stale = true, staled_at = COALESCE(rs.staled_at, now())
			WHERE rs.stale = false
			  AND EXISTS (SELECT 1 FROM clusters c
			              WHERE c.org_id = rs.org_id AND c.cluster_uuid = rs.cluster_uuid
			                AND c.last_reported_at IS NOT NULL)
			  AND NOT EXISTS (SELECT 1 FROM clusters c
			                  WHERE c.org_id = rs.org_id AND c.cluster_uuid = rs.cluster_uuid
			                    AND c.last_reported_at >= $1)
			RETURNING rs.org_id`,
			pgx.Identifier{table}.Sanitize())

		rows, err := pool.Query(ctx, query, livenessCutoff)
		if err != nil {
			errs = append(errs, fmt.Errorf("mark %s: %w", table, err))
			continue
		}
		for rows.Next() {
			var orgID string
			if err := rows.Scan(&orgID); err != nil {
				errs = append(errs, fmt.Errorf("scan %s marked org: %w", table, err))
				break
			}
			marked++
			if orgID != "" {
				affectedOrgs[orgID] = struct{}{}
			}
		}
		if err := rows.Err(); err != nil {
			errs = append(errs, fmt.Errorf("mark %s rows: %w", table, err))
		}
		rows.Close()
	}
	// #874: marking must reach the key catalogs, not only the caches. Default
	// list requests count and page through org_container_keys.is_stale and
	// org_namespace_keys (internal/model/native_list_keys.go,
	// native_ns_list_keys.go), and the in-cycle writer always follows
	// MarkUnreportedContainersStale with exactly these two refreshes
	// (internal/services/report_processor.go). Skipping them leaves meta.count
	// and filter[stale]=only reading a catalog that still believes the cluster is
	// alive: default lists then report a count larger than the rows they can
	// return, and walk past an invisible tail. Both calls are org-wide upserts,
	// so this costs one pass per affected org per sweep, not per row.
	for orgID := range affectedOrgs {
		if org := refreshOrgListCatalogs(ctx, pool, orgID); org != nil {
			errs = append(errs, org)
		}
	}
	return marked, errors.Join(errs...)
}

// refreshOrgListCatalogs rebuilds the org's list key catalogs and the caches that
// describe the fleet, after a sweep changed which rows are live.
//
// Both refreshes are required and both prune orphans (librobne/pgrec/metadata.go
// for containers, internal/model/org_namespace_keys.go for namespaces), so this
// also cleans up after a purge: without it a deleted cluster's catalog entries
// outlive its rows, and filter[stale]=only would count rows that no longer exist.
//
// A catalog failure falls back to cache invalidation so no cached summary keeps
// describing the fleet as it was before the sweep.
func refreshOrgListCatalogs(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	var errs []error
	if err := RefreshOrgMetadata(ctx, pool, orgID); err != nil {
		errs = append(errs, fmt.Errorf("refresh org metadata %s: %w", orgID, err))
		fleetsummary.InvalidateOrg(orgID)
		fleetheatmap.InvalidateOrg(orgID)
		clustercache.InvalidateOrg(orgID)
	}
	if err := model.RefreshOrgNamespaceKeys(ctx, pool, orgID); err != nil {
		errs = append(errs, fmt.Errorf("refresh org namespace keys %s: %w", orgID, err))
	}
	return errors.Join(errs...)
}
