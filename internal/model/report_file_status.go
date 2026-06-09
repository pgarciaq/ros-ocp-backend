package model

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ReportFilePending    = "pending"
	ReportFileProcessing = "processing"
	ReportFileDone       = "done"
	ReportFileFailed     = "failed"
)

// EnsureReportFileExpectations registers expected manifest files as pending rows.
func EnsureReportFileExpectations(
	ctx context.Context,
	pool *pgxpool.Pool,
	manifestID, clusterID, orgID string,
	expectedFiles []string,
	reportTypeFor func(filename string) string,
) error {
	if manifestID == "" || len(expectedFiles) == 0 {
		return nil
	}
	for _, filename := range expectedFiles {
		reportType := reportTypeFor(filename)
		_, err := pool.Exec(ctx, `
			INSERT INTO report_file_status (manifest_id, cluster_id, org_id, filename, report_type, status)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (manifest_id, filename) DO NOTHING`,
			manifestID, clusterID, orgID, filename, reportType, ReportFilePending,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetReportFileStatus returns the status for a manifest file, or empty string if not tracked.
func GetReportFileStatus(ctx context.Context, pool *pgxpool.Pool, manifestID, filename string) (string, error) {
	if manifestID == "" {
		return "", nil
	}
	var status string
	err := pool.QueryRow(ctx,
		`SELECT status FROM report_file_status WHERE manifest_id = $1 AND filename = $2`,
		manifestID, filename,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return status, err
}

// MarkReportFileProcessing sets a file to processing and records start time.
func MarkReportFileProcessing(
	ctx context.Context,
	pool *pgxpool.Pool,
	manifestID, clusterID, orgID, filename, reportType string,
) error {
	if manifestID == "" {
		return nil
	}
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		INSERT INTO report_file_status (manifest_id, cluster_id, org_id, filename, report_type, status, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (manifest_id, filename) DO UPDATE SET
			status = EXCLUDED.status,
			report_type = EXCLUDED.report_type,
			started_at = EXCLUDED.started_at,
			error_message = NULL,
			completed_at = NULL`,
		manifestID, clusterID, orgID, filename, reportType, ReportFileProcessing, now,
	)
	return err
}

// MarkReportFileDone marks a manifest file as successfully processed.
func MarkReportFileDone(ctx context.Context, pool *pgxpool.Pool, manifestID, filename string) error {
	if manifestID == "" {
		return nil
	}
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		UPDATE report_file_status
		SET status = $3, completed_at = $4, error_message = NULL
		WHERE manifest_id = $1 AND filename = $2`,
		manifestID, filename, ReportFileDone, now,
	)
	return err
}

// MarkReportFileFailed records a permanent file failure.
func MarkReportFileFailed(ctx context.Context, pool *pgxpool.Pool, manifestID, filename, errMsg string) error {
	if manifestID == "" {
		return nil
	}
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `
		UPDATE report_file_status
		SET status = $3, completed_at = $4, error_message = $5
		WHERE manifest_id = $1 AND filename = $2`,
		manifestID, filename, ReportFileFailed, now, errMsg,
	)
	return err
}

// IsManifestIngestionComplete returns true when every expected file for the manifest is done.
func IsManifestIngestionComplete(ctx context.Context, pool *pgxpool.Pool, manifestID string) (bool, error) {
	if manifestID == "" {
		return true, nil
	}
	var pending int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM report_file_status
		WHERE manifest_id = $1 AND status <> $2`,
		manifestID, ReportFileDone,
	).Scan(&pending)
	if err != nil {
		return false, err
	}
	var total int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM report_file_status WHERE manifest_id = $1`,
		manifestID,
	).Scan(&total)
	if err != nil {
		return false, err
	}
	return total > 0 && pending == 0, nil
}

// CompletedReportTypes returns distinct report types with done status for a manifest.
func CompletedReportTypes(ctx context.Context, pool *pgxpool.Pool, manifestID string) ([]string, error) {
	if manifestID == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT report_type FROM report_file_status
		WHERE manifest_id = $1 AND status = $2`,
		manifestID, ReportFileDone,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var rt string
		if err := rows.Scan(&rt); err != nil {
			return nil, err
		}
		types = append(types, rt)
	}
	return types, rows.Err()
}
