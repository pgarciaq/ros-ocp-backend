package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func partitionName(bucket time.Time) string {
	monthStart := monthUTC(bucket)
	return fmt.Sprintf("daily_container_digests_%s", monthStart.Format("200601"))
}

func monthUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// EnsurePartitionMonth creates daily_container_digests_YYYYMM if it does not exist.
func EnsurePartitionMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	monthStart = monthUTC(monthStart)
	monthEnd := monthStart.AddDate(0, 1, 0)
	part := pgx.Identifier{partitionName(monthStart)}.Sanitize()
	parent := pgx.Identifier{"daily_container_digests"}.Sanitize()
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
		part,
		parent,
		monthStart.Format("2006-01-02"),
		monthEnd.Format("2006-01-02"),
	)
	if _, err := pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("EnsurePartitionMonth %s: %w", partitionName(monthStart), err)
	}
	return nil
}

func ensurePartitionsForRows(ctx context.Context, pool *pgxpool.Pool, rows []Row) error {
	months := map[time.Time]struct{}{}
	for _, r := range rows {
		months[monthUTC(r.Digest.Row.BucketDate)] = struct{}{}
	}
	for monthStart := range months {
		if err := EnsurePartitionMonth(ctx, pool, monthStart); err != nil {
			return err
		}
	}
	return nil
}
