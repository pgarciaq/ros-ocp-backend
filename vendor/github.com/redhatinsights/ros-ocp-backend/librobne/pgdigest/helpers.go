package pgdigest

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func requireOrgCluster(orgID, clusterUUID string) error {
	if orgID == "" {
		return fmt.Errorf("pgdigest: org_id is required")
	}
	if clusterUUID == "" {
		return fmt.Errorf("pgdigest: cluster_uuid is required")
	}
	return nil
}

func requireQuerier(q Querier) error {
	if q == nil {
		return fmt.Errorf("pgdigest: querier is required")
	}
	return nil
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullInt64PodCapacity(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func withWriteTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin digest tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit digest tx: %w", err)
	}
	return nil
}

func flushQueued(ctx context.Context, sender pgxBatchSender, n int, queueEach func(batch *pgx.Batch, i int)) error {
	for chunkStart := 0; chunkStart < n; chunkStart += maxPgxBatchQueue {
		chunkEnd := min(chunkStart+maxPgxBatchQueue, n)
		batch := &pgx.Batch{}
		for i := chunkStart; i < chunkEnd; i++ {
			queueEach(batch, i)
		}
		if err := flushBatch(ctx, sender, batch); err != nil {
			return err
		}
	}
	return nil
}
