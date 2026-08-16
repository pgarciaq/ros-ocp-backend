package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func partitionName(bucket time.Time) string {
	return partitionChildName("daily_container_digests", bucket)
}

func partitionChildName(parent string, monthStart time.Time) string {
	return fmt.Sprintf("%s_%s", parent, monthUTC(monthStart).Format("200601"))
}

func monthUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// EnsurePartitionMonth creates daily_container_digests_YYYYMM if it does not exist.
func EnsurePartitionMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	return EnsureRangePartition(ctx, pool, "daily_container_digests", monthStart)
}

// EnsureRangePartition creates parent_YYYYMM PARTITION OF parent for the month of monthStart.
func EnsureRangePartition(ctx context.Context, pool *pgxpool.Pool, parent string, monthStart time.Time) error {
	monthStart = monthUTC(monthStart)
	monthEnd := monthStart.AddDate(0, 1, 0)
	child := partitionChildName(parent, monthStart)
	part := pgx.Identifier{child}.Sanitize()
	parentID := pgx.Identifier{parent}.Sanitize()
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
		part,
		parentID,
		monthStart.Format("2006-01-02"),
		monthEnd.Format("2006-01-02"),
	)
	if _, err := pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("EnsureRangePartition %s: %w", child, err)
	}
	return nil
}

func ensurePartitionsForRows(ctx context.Context, pool *pgxpool.Pool, rows []Row) error {
	months := make([]time.Time, 0, len(rows))
	for _, r := range rows {
		months = append(months, r.Digest.Row.BucketDate)
	}
	return ensureRangePartitions(ctx, pool, "daily_container_digests", months)
}

func ensureRangePartitions(ctx context.Context, pool *pgxpool.Pool, parent string, times []time.Time) error {
	months := map[time.Time]struct{}{}
	for _, t := range times {
		months[monthUTC(t)] = struct{}{}
	}
	for monthStart := range months {
		if err := EnsureRangePartition(ctx, pool, parent, monthStart); err != nil {
			return err
		}
	}
	return nil
}
