package plugin

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
)

// HookError pairs a hook name with the error it returned.
type HookError struct {
	HookName string
	Err      error
}

// FindCSVIngestor returns the first enabled CSVIngestor that claims csvType, or nil.
func FindCSVIngestor(csvType string) CSVIngestor {
	for _, ing := range ByTrait[CSVIngestor]() {
		for _, ct := range ing.SupportedCSVTypes() {
			if ct == csvType {
				return ing
			}
		}
	}
	return nil
}

// DispatchCSV finds the first enabled CSVIngestor that claims csvType, runs
// IngestCSV, then fires matching IngestHooks. Returns handled=false when no
// ingestor claims the type (caller should use a fallback path).
//
// Hook errors are collected in hookErrs (non-fatal by convention). The ingest
// error (if any) is returned as err.
func DispatchCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID, csvType string) (handled bool, rows []ingestion.MetricRow, hookErrs []HookError, err error) {
	matched := FindCSVIngestor(csvType)
	if matched == nil {
		return false, nil, nil, nil
	}

	rows, err = matched.IngestCSV(ctx, pool, r, orgID, clusterUUID)
	if err != nil {
		return true, nil, nil, err
	}

	if len(rows) > 0 {
		hookErrs = RunIngestHooks(ctx, pool, csvType, rows, orgID, clusterUUID)
	}

	return true, rows, hookErrs, nil
}

// RunIngestHooks fires all enabled IngestHooks that match csvType.
// Returns a slice of HookErrors for hooks that failed (non-fatal).
func RunIngestHooks(ctx context.Context, pool *pgxpool.Pool, csvType string, rows []ingestion.MetricRow, orgID, clusterUUID string) []HookError {
	hooks := ByTrait[IngestHook]()
	var errs []HookError
	for _, hook := range hooks {
		for _, ht := range hook.HookAfterCSVTypes() {
			if ht == csvType {
				if hookErr := hook.AfterIngest(ctx, pool, rows, orgID, clusterUUID); hookErr != nil {
					errs = append(errs, HookError{HookName: hook.Name(), Err: hookErr})
				}
				break
			}
		}
	}
	return errs
}
