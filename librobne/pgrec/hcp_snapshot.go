package pgrec

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

// SnapshotRow is one persisted HCP namespace mapping observation.
type SnapshotRow struct {
	ManifestID      string
	HCPNamespace    string
	HostedClusterID string
	HcUID           string
	HcpUID          string
	NamespaceUID    string
	NamespaceCreated time.Time
	ObservedAt      time.Time
	Complete        bool
	Diagnostics     string
}

// parseSnapshotTime parses an RFC3339 timestamp, yielding zero time on empty
// or unparseable input (callers treat zero as unknown, never as an error).
func parseSnapshotTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// nullTime converts zero times to NULL for storage.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// IsUndefinedTable reports missing-table/column errors from databases migrated
// before 000199 (code rollouts ahead of migrations). Callers degrade, never
// fail the run.
func IsUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "42P01" || pgErr.Code == "42703") {
		return true
	}
	return errors.Is(err, pgx.ErrNoRows)
}

// UpsertHCPSnapshots stores the report-scoped namespace-to-HC mapping by
// manifest identity (#621). One row per manifest x HCP namespace; replays
// overwrite. Empty entry lists persist nothing. Missing tables (pre-000199)
// are a silent no-op.
func UpsertHCPSnapshots(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, manifestID string, entries []topology.HCPSnapshotEntry) error {
	if len(entries) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, e := range entries {
		if e.HCPNamespace == "" {
			continue
		}
		observed := parseSnapshotTime(e.ObservedAt)
		if observed.IsZero() {
			observed = time.Now().UTC()
		}
		batch.Queue(`
			INSERT INTO manifest_hcp_snapshots (
				manifest_id, org_id, cluster_uuid, hcp_namespace,
				hosted_cluster_id, hc_uid, hcp_uid, namespace_uid,
				namespace_created_at, observed_at, complete, diagnostics
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (manifest_id, hcp_namespace)
			DO UPDATE SET
				hosted_cluster_id = EXCLUDED.hosted_cluster_id,
				hc_uid = EXCLUDED.hc_uid,
				hcp_uid = EXCLUDED.hcp_uid,
				namespace_uid = EXCLUDED.namespace_uid,
				namespace_created_at = EXCLUDED.namespace_created_at,
				observed_at = EXCLUDED.observed_at,
				complete = EXCLUDED.complete,
				diagnostics = EXCLUDED.diagnostics`,
			manifestID, orgID, clusterUUID, e.HCPNamespace,
			e.HostedClusterID, e.HcUID, e.HcpUID, e.NamespaceUID,
			nullTime(parseSnapshotTime(e.NamespaceCreatedAt)), observed,
			e.Complete, e.Diagnostics,
		)
	}
	if batch.Len() == 0 {
		return nil
	}
	br := pool.SendBatch(ctx, batch)
	defer br.Close()
	for range batch.Len() {
		if _, err := br.Exec(); err != nil {
			if IsUndefinedTable(err) {
				return nil
			}
			return fmt.Errorf("upsert hcp snapshots: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		if IsUndefinedTable(err) {
			return nil
		}
		return fmt.Errorf("upsert hcp snapshots batch close: %w", err)
	}
	return nil
}

// LoadHCPSnapshotsForRun returns mapping observations at or after since for
// org+cluster, oldest first. Missing tables yield empty with nil error.
func LoadHCPSnapshotsForRun(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, since time.Time) ([]SnapshotRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT manifest_id, hcp_namespace,
		       COALESCE(hosted_cluster_id, ''), COALESCE(hc_uid, ''),
		       COALESCE(hcp_uid, ''), COALESCE(namespace_uid, ''),
		       namespace_created_at, observed_at, complete,
		       COALESCE(diagnostics, '')
		FROM manifest_hcp_snapshots
		WHERE org_id = $1 AND cluster_uuid = $2 AND observed_at >= $3
		ORDER BY observed_at ASC`, orgID, clusterUUID, since)
	if err != nil {
		if IsUndefinedTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load hcp snapshots: %w", err)
	}
	defer rows.Close()
	var out []SnapshotRow
	for rows.Next() {
		var r SnapshotRow
		var nsCreated, observed *time.Time
		if err := rows.Scan(&r.ManifestID, &r.HCPNamespace,
			&r.HostedClusterID, &r.HcUID, &r.HcpUID, &r.NamespaceUID,
			&nsCreated, &observed, &r.Complete, &r.Diagnostics); err != nil {
			return nil, fmt.Errorf("scan hcp snapshot: %w", err)
		}
		// NULL timestamps arrive as nil pointers; normalize to zero time.
		if nsCreated != nil {
			r.NamespaceCreated = *nsCreated
		}
		if observed != nil {
			r.ObservedAt = *observed
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hcp snapshots: %w", err)
	}
	return out, nil
}
