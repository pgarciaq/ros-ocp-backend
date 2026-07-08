package ingestion

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const (
	deadlockRetryAttempts = 3
	deadlockRetryBaseMs   = 50
)

// isDeadlock returns true if the error is a PostgreSQL deadlock (SQLSTATE 40P01).
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// withDeadlockRetry retries fn up to deadlockRetryAttempts times on PostgreSQL
// deadlocks, with exponential backoff (50ms, 100ms, 200ms). Non-deadlock errors
// are returned immediately.
//
// Deadlocks occur when concurrent transactions lock the same rows in different
// orders during INSERT ON CONFLICT. The primary prevention is deterministic key
// sorting (see sortDigestKeys, etc.), but retries provide defense-in-depth for
// edge cases like autovacuum contention.
func withDeadlockRetry(label string, fn func() error) error {
	for attempt := range deadlockRetryAttempts {
		err := fn()
		if err == nil {
			return nil
		}
		if !isDeadlock(err) {
			return err
		}
		if attempt == deadlockRetryAttempts-1 {
			return fmt.Errorf("%s: deadlock after %d attempts: %w", label, deadlockRetryAttempts, err)
		}
		backoff := time.Duration(deadlockRetryBaseMs*(1<<attempt)) * time.Millisecond
		logging.GetLogger().Warnf("%s: deadlock detected (attempt %d/%d), retrying in %v",
			label, attempt+1, deadlockRetryAttempts, backoff)
		time.Sleep(backoff)
	}
	return nil
}
