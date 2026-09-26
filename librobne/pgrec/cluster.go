package pgrec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

// SourceID is the clusters.source_id value this CLI writes. Any other value
// on any clusters row means the database is not CLI-owned (Helm/Sources).
const SourceID = "robne"

// ErrForeignSourceID is returned when clusters already has a non-robne source_id.
var ErrForeignSourceID = fmt.Errorf("database has clusters.source_id other than %q; refusing write (looks like Helm/Sources)", SourceID)

// AssertCLIOwned refuses the whole write if any clusters row is not source_id=robne.
func AssertCLIOwned(ctx context.Context, pool *pgxpool.Pool) error {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM clusters WHERE source_id IS DISTINCT FROM $1`, SourceID).Scan(&n)
	if err != nil {
		return fmt.Errorf("check clusters.source_id: %w", err)
	}
	if n > 0 {
		return ErrForeignSourceID
	}
	return nil
}

// EnsureAccountCluster inserts rh_accounts and clusters for YAML identity.
// cluster_alias is the cluster UUID string (clusters.cluster_alias is NOT NULL).
func EnsureAccountCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, lastReported time.Time) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("ensure clusters: org_id is required")
	}
	if lastReported.IsZero() {
		lastReported = time.Now().UTC()
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO NOTHING`, orgID)
	if err != nil {
		return fmt.Errorf("ensure rh_accounts: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, org_id, source_id, cluster_uuid, cluster_alias, last_reported_at)
		SELECT ra.id, $1, $2, $3, $4, $5
		FROM rh_accounts ra
		WHERE ra.org_id = $1
		ON CONFLICT (tenant_id, source_id, cluster_uuid, cluster_alias)
		DO UPDATE SET last_reported_at = EXCLUDED.last_reported_at,
		              org_id = EXCLUDED.org_id`,
		orgID, SourceID, clusterUUID, clusterUUID, lastReported,
	)
	if err != nil {
		return fmt.Errorf("ensure clusters: %w", err)
	}
	return nil
}

// EnsureIngestClusterRow creates the rh_accounts + clusters rows for an
// ingest message when source-sync hasn't yet (new clusters), so per-cycle
// topology/HCP persists land from the first enriched message instead of
// no-op'ing on the missing row (#612). Idempotent: ON CONFLICT DO NOTHING
// on the same keys source-sync uses; existing rows are never modified.
// lastReported defaults to now when zero. Unlike EnsureAccountCluster
// (CLI-owned, fixed SourceID), the source and alias come from the message.
// Callers keep warn-and-continue: a failed ensure must not fail the run.
func EnsureIngestClusterRow(ctx context.Context, pool *pgxpool.Pool, orgID, sourceID, clusterUUID, clusterAlias string, lastReported time.Time) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("ensure ingest clusters: org_id is required")
	}
	if strings.TrimSpace(sourceID) == "" {
		return fmt.Errorf("ensure ingest clusters: source_id is required")
	}
	if strings.TrimSpace(clusterUUID) == "" {
		return fmt.Errorf("ensure ingest clusters: cluster_uuid is required")
	}
	if strings.TrimSpace(clusterAlias) == "" {
		return fmt.Errorf("ensure ingest clusters: cluster_alias is required")
	}
	if lastReported.IsZero() {
		lastReported = time.Now().UTC()
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO NOTHING`, orgID)
	if err != nil {
		return fmt.Errorf("ensure ingest rh_accounts: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, org_id, source_id, cluster_uuid, cluster_alias, last_reported_at)
		SELECT ra.id, $1, $2, $3, $4, $5
		FROM rh_accounts ra
		WHERE ra.org_id = $1
		ON CONFLICT (tenant_id, source_id, cluster_uuid, cluster_alias) DO NOTHING`,
		orgID, sourceID, clusterUUID, clusterAlias, lastReported,
	)
	if err != nil {
		return fmt.Errorf("ensure ingest clusters: %w", err)
	}
	return nil
}

// ReadClusterTopology returns the stored W0 classification for a cluster,
// preferring a non-unknown row when several sources share org+uuid.
// Missing rows — and databases migrated before 000196 — yield unknown with
// nil error: callers must never fail a recommendation run on topology.
func ReadClusterTopology(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (topology.ClusterTopology, error) {
	var v string
	err := pool.QueryRow(ctx, `
		SELECT c.cluster_topology FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2
		ORDER BY (c.cluster_topology = 'unknown'), c.last_reported_at DESC NULLS LAST
		LIMIT 1`, orgID, clusterUUID).Scan(&v)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return topology.TopologyUnknown, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42703" {
			// Undefined column: database migrated before 000196 (code
			// rollouts ahead of migrations). Degrade, never fail the run.
			return topology.TopologyUnknown, nil
		}
		return topology.TopologyUnknown, fmt.Errorf("read cluster_topology: %w", err)
	}
	return topology.ParseClusterTopology(v), nil
}

// UpdateClusterTopology stores the W0 topology classification on the clusters
// row (W0.2, #407). The value is normalized through String(): bogus input
// persists as unknown, never raw. Missing rows are a silent no-op; callers
// ensure the row first (see EnsureAccountCluster call sites). sourceID scopes
// the write to the caller's tenancy (CLI passes SourceID).
func UpdateClusterTopology(ctx context.Context, pool *pgxpool.Pool, orgID, sourceID, clusterUUID string, topo topology.ClusterTopology) error {
	_, err := pool.Exec(ctx, `
		UPDATE clusters SET cluster_topology = $4
		FROM rh_accounts ra
		WHERE clusters.tenant_id = ra.id AND ra.org_id = $1
		  AND clusters.source_id = $2 AND clusters.cluster_uuid = $3`,
		orgID, sourceID, clusterUUID, topo.String(),
	)
	if err != nil {
		return fmt.Errorf("update cluster_topology: %w", err)
	}
	return nil
}

// ReadHCPNamespaces returns the stored W1.1 hosted-control-plane namespace
// list for a cluster, preferring a non-empty row when several sources share
// org+uuid. Missing rows — and databases migrated before 000198 — yield an
// empty list with nil error: callers must never fail a recommendation run on
// persisted HCP state (guardrail routing degrades to off).
func ReadHCPNamespaces(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]string, error) {
	var v []string
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(c.hcp_namespaces, '{}') FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2
		ORDER BY (c.hcp_namespaces = '{}'), c.last_reported_at DESC NULLS LAST
		LIMIT 1`, orgID, clusterUUID).Scan(&v)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42703" {
			// Undefined column: database migrated before 000198 (code
			// rollouts ahead of migrations). Degrade, never fail the run.
			return nil, nil
		}
		return nil, fmt.Errorf("read hcp_namespaces: %w", err)
	}
	return v, nil
}

// UpdateHCPNamespaces stores the W1.1 hosted-control-plane namespace list on
// the clusters row, replacing any previous list on conflict (per-cycle
// overwrite; an empty list clears stored namespaces). Missing rows are a
// silent no-op; callers ensure the row first (see persistTopologyFacts call
// sites). The column is NOT NULL, so nil normalizes to an empty list. sourceID
// scopes the write to the caller's tenancy.
func UpdateHCPNamespaces(ctx context.Context, pool *pgxpool.Pool, orgID, sourceID, clusterUUID string, namespaces []string) error {
	if namespaces == nil {
		namespaces = []string{}
	}
	_, err := pool.Exec(ctx, `
		UPDATE clusters SET hcp_namespaces = $4
		FROM rh_accounts ra
		WHERE clusters.tenant_id = ra.id AND ra.org_id = $1
		  AND clusters.source_id = $2 AND clusters.cluster_uuid = $3`,
		orgID, sourceID, clusterUUID, namespaces,
	)
	if err != nil {
		return fmt.Errorf("update hcp_namespaces: %w", err)
	}
	return nil
}
