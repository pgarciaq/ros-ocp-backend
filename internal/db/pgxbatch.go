package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PgxBatchSender matches *pgxpool.Pool and pgx.Tx for SendBatch.
type PgxBatchSender interface {
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

// FlushRecommendationBatch sends a pgx.Batch and checks each statement result.
func FlushRecommendationBatch(ctx context.Context, sender PgxBatchSender, batch *pgx.Batch) error {
	n := batch.Len()
	br := sender.SendBatch(ctx, batch)
	defer br.Close()
	for i := range n {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("FlushRecommendationBatch: statement %d/%d: %w", i+1, n, err)
		}
	}
	return nil
}
