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
	case "gpu_model":
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

// CountNodeGPUTriples returns the number of distinct (cluster, node, gpu_model) groups visible to the org.
func CountNodeGPUTriples(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, start, end time.Time, nodeContains, gpuContains string) (int, error) {
	if len(clusterUUIDs) == 0 {
		return 0, nil
	}
	startD := start.Format("2006-01-02")
	endD := end.Format("2006-01-02")
	q := `
SELECT COUNT(*) FROM (
  SELECT g.cluster_uuid, g.node_name, g.gpu_model_name
  FROM gpu_container_digests g
  INNER JOIN clusters c ON c.cluster_uuid::text = g.cluster_uuid::text
  INNER JOIN rh_accounts ra ON ra.id = c.tenant_id
  WHERE ra.org_id = $1
    AND g.interval_start >= $2::date AND g.interval_start <= $3::date
    AND g.cluster_uuid::text = ANY($4::text[])
    AND ($5::text = '' OR LOWER(TRIM(g.node_name)) = LOWER(TRIM($5)))
    AND ($6::text = '' OR STRPOS(LOWER(g.gpu_model_name), LOWER($6)) > 0)
  GROUP BY g.cluster_uuid, g.node_name, g.gpu_model_name
) sub`
	var n int
	err := pool.QueryRow(ctx, q, orgID, startD, endD, clusterUUIDs, nodeContains, gpuContains).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count node GPU triples: %w", err)
	}
	return n, nil
}

// ListNodeGPUTriplesPage returns one page of distinct (cluster, node, gpu_model) keys.
func ListNodeGPUTriplesPage(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, start, end time.Time, nodeContains, gpuContains string, orderByColumn string, orderDesc bool, limit, offset int) ([]NodeGPUTriple, error) {
	if len(clusterUUIDs) == 0 {
		return nil, nil
	}
	orderSQL := gpuTripleOrderExpr(orderByColumn, orderDesc)
	startD := start.Format("2006-01-02")
	endD := end.Format("2006-01-02")
	q := `
SELECT g.cluster_uuid::text, g.node_name, g.gpu_model_name
FROM gpu_container_digests g
INNER JOIN clusters c ON c.cluster_uuid::text = g.cluster_uuid::text
INNER JOIN rh_accounts ra ON ra.id = c.tenant_id
WHERE ra.org_id = $1
  AND g.interval_start >= $2::date AND g.interval_start <= $3::date
  AND g.cluster_uuid::text = ANY($4::text[])
  AND ($5::text = '' OR LOWER(TRIM(g.node_name)) = LOWER(TRIM($5)))
  AND ($6::text = '' OR STRPOS(LOWER(g.gpu_model_name), LOWER($6)) > 0)
GROUP BY g.cluster_uuid, g.node_name, g.gpu_model_name
ORDER BY ` + orderSQL + `
LIMIT $7 OFFSET $8`

	rows, err := pool.Query(ctx, q, orgID, startD, endD, clusterUUIDs, nodeContains, gpuContains, limit, offset)
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
	case "node_name", "cluster_uuid", "gpu_model":
		return true
	default:
		return false
	}
}
