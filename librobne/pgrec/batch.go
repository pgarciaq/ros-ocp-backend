package pgrec

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const maxPgxBatchQueue = 2000

type pgxBatchSender interface {
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

func flushBatch(ctx context.Context, sender pgxBatchSender, batch *pgx.Batch) error {
	n := batch.Len()
	br := sender.SendBatch(ctx, batch)
	defer br.Close()
	for i := range n {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("pgrec flush: statement %d/%d: %w", i+1, n, err)
		}
	}
	return nil
}
