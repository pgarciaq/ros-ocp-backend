package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
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

// Tables retained by the general ROS_RETENTION_MONTHS setting.
// This fallback list is used when no RetentionProvider plugins are registered.
// When plugins ARE registered, each plugin sweeps its own tables via SweepRetention.
var retainedTables = []string{
	"container_usage_samples",
	"daily_container_digests",
	"daily_namespace_digests",
	"daily_node_digests",
	"gpu_container_digests",
	"namespace_usage_samples",
	"node_recommendations",
}

// Tables retained by the separate ROS_HISTORY_RETENTION_DAYS setting
// (history/quality grow faster: one row per container×term×engine per run).
var historyRetainedTables = []string{
	"recommendation_history",
	"recommendation_quality",
}

// RetentionTable is a compile-time-only struct for non-partitioned tables that need
// date-based DELETE retention. Both Table and DateColumn are hard-coded identifiers
// (never from user input) — the fmt.Sprintf interpolation is safe by construction.
type RetentionTable struct {
	Table      string
	DateColumn string
}

var dateRetainedTables = []RetentionTable{
	{"historical_namespace_recommendation_sets", "created_at"},
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
	if err := SweepPartitionedTables(ctx, pool, historyRetainedTables, historyCutoffYM); err != nil {
		logging.GetLogger().Warnf("retention: partitioned sweep (history): %v", err)
		errs = append(errs, err)
	}

	for _, dt := range dateRetainedTables {
		sql := fmt.Sprintf("DELETE FROM %s WHERE %s < $1", dt.Table, dt.DateColumn)
		tag, err := pool.Exec(ctx, sql, cutoff)
		if err != nil {
			logging.GetLogger().Warnf("retention: purging %s: %v", dt.Table, err)
			errs = append(errs, fmt.Errorf("purge %s: %w", dt.Table, err))
		} else if tag.RowsAffected() > 0 {
			retentionDropped.Add(float64(tag.RowsAffected()))
			logging.GetLogger().Infof("retention: purged %d rows from %s (older than %s)", tag.RowsAffected(), dt.Table, cutoff.Format("2006-01-02"))
		}
	}

	// Stale recommendation cleanup: delete stale recommendations that haven't
	// received new data in staleCleanupDays (default 30 days).
	staleCleanupDays := cfg.StaleCleanupDays
	if staleCleanupDays <= 0 {
		staleCleanupDays = 30
	}
	staleCutoff := time.Now().UTC().AddDate(0, 0, -staleCleanupDays)
	purgedStale, invalidateErr := purgeStaleRecommendations(ctx, pool, staleCutoff)
	if invalidateErr != nil {
		logging.GetLogger().Warnf("retention: purging stale recommendations: %v", invalidateErr)
		errs = append(errs, fmt.Errorf("purge stale recommendations: %w", invalidateErr))
	} else if purgedStale > 0 {
		retentionDropped.Add(float64(purgedStale))
		logging.GetLogger().Infof("retention: purged %d stale recommendations (older than %s)", purgedStale, staleCutoff.Format("2006-01-02"))
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

func purgeStaleRecommendations(ctx context.Context, pool *pgxpool.Pool, staleCutoff time.Time) (int64, error) {
	rows, err := pool.Query(ctx,
		"DELETE FROM recommendation_sets WHERE stale = true AND updated_at < $1 RETURNING org_id",
		staleCutoff,
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
	}
	return purged, nil
}

// SweepPartitionedTables drops monthly partitions older than cutoffYM (YYYYMM) for each parent table.
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
			sql := fmt.Sprintf("DROP TABLE IF EXISTS %s", part)
			if _, err := pool.Exec(ctx, sql); err != nil {
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
