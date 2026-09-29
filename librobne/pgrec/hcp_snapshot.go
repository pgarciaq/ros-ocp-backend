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
	ManifestID       string
	HCPNamespace     string
	HostedClusterID  string
	HcUID            string
	HcpUID           string
	NamespaceUID     string
	NamespaceCreated time.Time
	ObservedAt       time.Time
	Complete         bool
	Diagnostics      string
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

// ResolveHCAssociation maps HCP namespaces to hosted cluster IDs from window
// observations (#621 temporal rule). A namespace associates iff at least one
// complete row proves a hosted ID and every complete row agrees on one
// (ID, incarnation-UID) tuple. Incomplete rows, empty IDs, conflicts, and UID
// changes (HC3 recreated as HC5) all yield absence — never a guess. Missing
// UIDs are lenient (same ID collapses); present-but-differing UIDs are strict
// (recreation detected).
func ResolveHCAssociation(snaps []SnapshotRow) map[string]string {
	byNS := make(map[string][]SnapshotRow)
	for _, s := range snaps {
		if s.HCPNamespace == "" {
			continue
		}
		byNS[s.HCPNamespace] = append(byNS[s.HCPNamespace], s)
	}
	out := make(map[string]string)
	for ns, rows := range byNS {
		seen := make(map[[2]string]bool)
		var hc string
		proven := 0
		for _, r := range rows {
			if !r.Complete || r.HostedClusterID == "" {
				continue
			}
			proven++
			key := [2]string{r.HostedClusterID, r.HcUID}
			if !seen[key] {
				seen[key] = true
				hc = r.HostedClusterID
			}
		}
		if proven > 0 && len(seen) == 1 {
			out[ns] = hc
		}
	}
	return out
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

// LoadHCPNamespaceSetForRun returns the distinct HCP namespaces with at
// least one complete snapshot observation at or after since, sorted.
// It feeds guardrail routing (unioned with the clusters-row list by callers):
// evidence presence, not proof — association unanimity lives in
// ResolveHCAssociation. Missing tables yield empty with nil error.
func LoadHCPNamespaceSetForRun(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, since time.Time) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT hcp_namespace FROM manifest_hcp_snapshots
		WHERE org_id = $1 AND cluster_uuid = $2 AND observed_at >= $3 AND complete = TRUE
		ORDER BY hcp_namespace ASC`, orgID, clusterUUID, since)
	if err != nil {
		if IsUndefinedTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load hcp namespace set: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ns string
		if err := rows.Scan(&ns); err != nil {
			return nil, fmt.Errorf("scan hcp namespace: %w", err)
		}
		out = append(out, ns)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hcp namespaces: %w", err)
	}
	return out, nil
}

// org+cluster, oldest first. Missing tables yield empty with nil error.
// LoadHCPSnapshotsForRun returns mapping observations at or after since for
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
