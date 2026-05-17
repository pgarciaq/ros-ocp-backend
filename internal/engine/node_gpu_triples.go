package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NodeGPUTriple identifies a distinct GPU-bearing node × model within a cluster for listing/pagination.
type NodeGPUTriple struct {
	ClusterUUID string
	NodeName    string
	GPUModel    string
}

func gpuTripleOrderExpr(orderByColumn string, desc bool) string {
	var col string
	switch orderByColumn {
	case "cluster_uuid":
		col = "g.cluster_uuid::text"
	case "gpu_model", "gpu_model_name":
		col = "g.gpu_model_name"
	default:
		col = "g.node_name"
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	// Stable tie-breakers for pagination.
	return fmt.Sprintf("%s %s NULLS LAST, g.cluster_uuid::text ASC, g.gpu_model_name ASC", col, dir)
}

// CountNodeGPUTriples returns the number of distinct (cluster, node, gpu_model) groups visible to the org,
// after excluding nodes whose latest digest row is older than the GPU freshness window (aligned with ComputeNodeTimeslicingRec).
func CountNodeGPUTriples(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, start, end, now time.Time, nodeContains, gpuContains string) (int, error) {
	if len(clusterUUIDs) == 0 {
		return 0, nil
	}
	startD := start.Format("2006-01-02")
	endD := end.Format("2006-01-02")
	cutoff := now.Add(-time.Duration(NodeGPUFreshnessDays) * 24 * time.Hour)
	q := `
SELECT COUNT(*) FROM (
  SELECT g.cluster_uuid, g.node_name, g.gpu_model_name
  FROM gpu_container_digests g
  INNER JOIN clusters c ON c.cluster_uuid::text = g.cluster_uuid::text
  INNER JOIN rh_accounts ra ON ra.id = c.tenant_id
  INNER JOIN (
    SELECT g3.cluster_uuid, g3.node_name
    FROM gpu_container_digests g3
    INNER JOIN clusters c3 ON c3.cluster_uuid::text = g3.cluster_uuid::text
    INNER JOIN rh_accounts ra3 ON ra3.id = c3.tenant_id
    WHERE ra3.org_id = $1
      AND g3.interval_start >= $2::date AND g3.interval_start <= $3::date
      AND g3.cluster_uuid::text = ANY($4::text[])
    GROUP BY g3.cluster_uuid, g3.node_name
    HAVING MAX(g3.interval_start) >= $7::timestamptz
  ) fresh ON fresh.cluster_uuid = g.cluster_uuid AND fresh.node_name = g.node_name
  WHERE ra.org_id = $1
    AND g.interval_start >= $2::date AND g.interval_start <= $3::date
    AND g.cluster_uuid::text = ANY($4::text[])
    AND ($5::text = '' OR LOWER(TRIM(g.node_name)) = LOWER(TRIM($5)))
    AND ($6::text = '' OR STRPOS(LOWER(g.gpu_model_name), LOWER($6)) > 0)
  GROUP BY g.cluster_uuid, g.node_name, g.gpu_model_name
) sub`
	var n int
	err := pool.QueryRow(ctx, q, orgID, startD, endD, clusterUUIDs, nodeContains, gpuContains, cutoff).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count node GPU triples: %w", err)
	}
	return n, nil
}

// ListNodeGPUTriplesPage returns one page of distinct (cluster, node, gpu_model) keys
// after excluding nodes outside the GPU freshness window (see CountNodeGPUTriples).
func ListNodeGPUTriplesPage(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, start, end, now time.Time, nodeContains, gpuContains string, orderByColumn string, orderDesc bool, limit, offset int) ([]NodeGPUTriple, error) {
	if len(clusterUUIDs) == 0 {
		return nil, nil
	}
	orderSQL := gpuTripleOrderExpr(orderByColumn, orderDesc)
	startD := start.Format("2006-01-02")
	endD := end.Format("2006-01-02")
	cutoff := now.Add(-time.Duration(NodeGPUFreshnessDays) * 24 * time.Hour)
	q := `
SELECT g.cluster_uuid::text, g.node_name, g.gpu_model_name
FROM gpu_container_digests g
INNER JOIN clusters c ON c.cluster_uuid::text = g.cluster_uuid::text
INNER JOIN rh_accounts ra ON ra.id = c.tenant_id
INNER JOIN (
  SELECT g3.cluster_uuid, g3.node_name
  FROM gpu_container_digests g3
  INNER JOIN clusters c3 ON c3.cluster_uuid::text = g3.cluster_uuid::text
  INNER JOIN rh_accounts ra3 ON ra3.id = c3.tenant_id
  WHERE ra3.org_id = $1
    AND g3.interval_start >= $2::date AND g3.interval_start <= $3::date
    AND g3.cluster_uuid::text = ANY($4::text[])
  GROUP BY g3.cluster_uuid, g3.node_name
  HAVING MAX(g3.interval_start) >= $7::timestamptz
) fresh ON fresh.cluster_uuid = g.cluster_uuid AND fresh.node_name = g.node_name
WHERE ra.org_id = $1
  AND g.interval_start >= $2::date AND g.interval_start <= $3::date
  AND g.cluster_uuid::text = ANY($4::text[])
  AND ($5::text = '' OR LOWER(TRIM(g.node_name)) = LOWER(TRIM($5)))
  AND ($6::text = '' OR STRPOS(LOWER(g.gpu_model_name), LOWER($6)) > 0)
GROUP BY g.cluster_uuid, g.node_name, g.gpu_model_name
ORDER BY ` + orderSQL + `
LIMIT $8 OFFSET $9`

	rows, err := pool.Query(ctx, q, orgID, startD, endD, clusterUUIDs, nodeContains, gpuContains, cutoff, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list node GPU triples page: %w", err)
	}
	defer rows.Close()

	var out []NodeGPUTriple
	for rows.Next() {
		var t NodeGPUTriple
		if err := rows.Scan(&t.ClusterUUID, &t.NodeName, &t.GPUModel); err != nil {
			return nil, fmt.Errorf("scan node GPU triple: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GPUOrderColumnSupportsTriplePagination reports whether ORDER BY can be pushed down to gpu_container_digests grouping.
func GPUOrderColumnSupportsTriplePagination(orderByColumn string) bool {
	switch strings.TrimSpace(orderByColumn) {
	case "node_name", "cluster_uuid", "gpu_model", "gpu_model_name":
		return true
	default:
		return false
	}
}

// CountOrgGPUClusterStats returns how many distinct clusters have rows in gpu_container_digests
// and how many distinct (cluster_uuid, node_name, gpu_model_name) triples exist (no freshness filter).
func CountOrgGPUClusterStats(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string) (distinctClusters int, distinctTriples int, err error) {
	if len(clusterUUIDs) == 0 {
		return 0, 0, nil
	}
	qClusters := `
SELECT COUNT(DISTINCT g.cluster_uuid)::int
FROM gpu_container_digests g
INNER JOIN clusters c ON c.cluster_uuid::text = g.cluster_uuid::text
INNER JOIN rh_accounts ra ON ra.id = c.tenant_id
WHERE ra.org_id = $1
  AND g.cluster_uuid::text = ANY($2::text[])`
	var dc int
	if err := pool.QueryRow(ctx, qClusters, orgID, clusterUUIDs).Scan(&dc); err != nil {
		return 0, 0, fmt.Errorf("count org GPU clusters: %w", err)
	}

	qTriples := `
SELECT COUNT(*)::int FROM (
  SELECT DISTINCT g.cluster_uuid, g.node_name, g.gpu_model_name
  FROM gpu_container_digests g
  INNER JOIN clusters c ON c.cluster_uuid::text = g.cluster_uuid::text
  INNER JOIN rh_accounts ra ON ra.id = c.tenant_id
  WHERE ra.org_id = $1
    AND g.cluster_uuid::text = ANY($2::text[])
) sub`
	var dt int
	if err := pool.QueryRow(ctx, qTriples, orgID, clusterUUIDs).Scan(&dt); err != nil {
		return 0, 0, fmt.Errorf("count org GPU triples: %w", err)
	}
	return dc, dt, nil
}
