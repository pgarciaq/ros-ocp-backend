package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	log "github.com/sirupsen/logrus"
)

var retentionDropped = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_retention_partitions_dropped_total",
	Help: "Number of partitions dropped by retention sweep",
})

// Tables retained by the general ROS_RETENTION_MONTHS setting.
var retainedTables = []string{
	"container_usage_samples",
	"daily_container_digests",
	"daily_namespace_digests",
	"gpu_container_digests",
	"namespace_usage_samples",
}

// Tables retained by the separate ROS_HISTORY_RETENTION_DAYS setting
// (history/quality grow faster: one row per container×term×engine per run).
var historyRetainedTables = []string{
	"recommendation_history",
	"recommendation_quality",
}

// Non-partitioned tables that need date-based DELETE retention.
var dateRetainedTables = []struct {
	Table      string
	DateColumn string
}{
	{"historical_namespace_recommendation_sets", "created_at"},
}

// RunRetentionSweep drops monthly partitions older than retentionMonths for
// each retained table. History/quality tables use historyRetentionDays instead
// (default 90 days). Partitions are identified by naming convention
// (<table>_YYYYMM) and compared against the retention cutoff. Also purges
// old rows from non-partitioned tables that use date-based retention.
func RunRetentionSweep(ctx context.Context, pool *pgxpool.Pool, retentionMonths int) {
	if retentionMonths <= 0 {
		retentionMonths = 6
	}

	cutoff := time.Now().UTC().AddDate(0, -retentionMonths, 0)
	cutoffYM := cutoff.Format("200601")

	sweepPartitionedTables(ctx, pool, retainedTables, cutoffYM)

	cfg := config.GetConfig()
	historyDays := cfg.HistoryRetentionDays
	if historyDays <= 0 {
		historyDays = 90
	}
	historyCutoff := time.Now().UTC().AddDate(0, 0, -historyDays)
	historyCutoffYM := historyCutoff.Format("200601")
	sweepPartitionedTables(ctx, pool, historyRetainedTables, historyCutoffYM)

	for _, dt := range dateRetainedTables {
		sql := fmt.Sprintf("DELETE FROM %s WHERE %s < $1", dt.Table, dt.DateColumn)
		tag, err := pool.Exec(ctx, sql, cutoff)
		if err != nil {
			log.Warnf("retention: purging %s: %v", dt.Table, err)
		} else if tag.RowsAffected() > 0 {
			retentionDropped.Add(float64(tag.RowsAffected()))
			log.Infof("retention: purged %d rows from %s (older than %s)", tag.RowsAffected(), dt.Table, cutoff.Format("2006-01-02"))
		}
	}

	// Stale recommendation archive: delete stale recommendations that haven't
	// received new data in staleArchiveDays (default 30 days).
	staleArchiveDays := cfg.StaleArchiveDays
	if staleArchiveDays <= 0 {
		staleArchiveDays = 30
	}
	staleCutoff := time.Now().UTC().AddDate(0, 0, -staleArchiveDays)
	tag, err := pool.Exec(ctx,
		"DELETE FROM recommendation_sets WHERE stale = true AND updated_at < $1",
		staleCutoff,
	)
	if err != nil {
		log.Warnf("retention: purging stale recommendations: %v", err)
	} else if tag.RowsAffected() > 0 {
		retentionDropped.Add(float64(tag.RowsAffected()))
		log.Infof("retention: purged %d stale recommendations (older than %s)", tag.RowsAffected(), staleCutoff.Format("2006-01-02"))
	}
}

func sweepPartitionedTables(ctx context.Context, pool *pgxpool.Pool, tables []string, cutoffYM string) {
	for _, table := range tables {
		partitions, err := listPartitions(ctx, pool, table)
		if err != nil {
			log.Warnf("retention: listing partitions for %s: %v", table, err)
			continue
		}
		for _, part := range partitions {
			ym := extractYearMonth(part, table)
			if ym == "" || ym >= cutoffYM {
				continue
			}
			sql := fmt.Sprintf("DROP TABLE IF EXISTS %s", part)
			if _, err := pool.Exec(ctx, sql); err != nil {
				log.Warnf("retention: dropping %s: %v", part, err)
			} else {
				retentionDropped.Inc()
				log.Infof("retention: dropped partition %s (older than %s)", part, cutoffYM)
			}
		}
	}
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
	RunRetentionSweep(ctx, pool, retentionMonths)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RunRetentionSweep(ctx, pool, retentionMonths)
		}
	}
}
