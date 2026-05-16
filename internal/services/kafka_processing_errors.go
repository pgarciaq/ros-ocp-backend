package services

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// isTransientKafkaProcessingError classifies errors that should prevent committing
// the Kafka offset so the consumer can retry (DB outages, timeouts, etc.).
//
// Safe default: unknown / unclassified errors are treated as transient (returns true)
// so offsets are not committed and Kafka can redeliver.
//
// Explicit non-transient cases (returns false) allow offset commit: pgx.ErrNoRows and
// PostgreSQL class 22 (data exception) / class 23 (integrity constraint violation).
func isTransientKafkaProcessingError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Connection failures, serialization failures, deadlocks, statement timeouts.
		switch pgErr.Code {
		case "08000", "08003", "08006", "08001", "08004", "57P01", "57P02", "57P03":
			return true
		case "40001", "40P01": // serialization_failure, deadlock_detected
			return true
		case "57014": // query_canceled (often timeout)
			return true
		}
		// Bad data / constraint violations — commit offset (do not retry this payload forever).
		if len(pgErr.Code) >= 2 {
			switch pgErr.Code[:2] {
			case "22": // Class 22 — Data Exception
				return false
			case "23": // Class 23 — Integrity Constraint Violation
				return false
			}
		}
	}
	// pgx pool / connection wrapper errors
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "server closed the connection") {
		return true
	}
	return true
}
