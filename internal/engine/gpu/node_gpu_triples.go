package gpu

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
// clusterUUIDs must already be scoped to the org (from getClustersForOrg + RBAC).
func CountNodeGPUTriples(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, start, end, now time.Time, nodeContains, gpuContains string) (int, error) {
	if len(clusterUUIDs) == 0 {
		return 0, nil
	}
	startD := start.Format("2006-01-02")
	endD := end.Format("2006-01-02")
	cutoff := now.Add(-time.Duration(CurrentGPUThresholdSettings().NodeFreshnessDays) * 24 * time.Hour)
	q := `
SELECT COUNT(*) FROM (
  SELECT g.cluster_uuid, g.node_name, g.gpu_model_name
  FROM gpu_container_digests g
  INNER JOIN (
    SELECT g3.cluster_uuid, g3.node_name
    FROM gpu_container_digests g3
    WHERE g3.org_id = $7
      AND g3.interval_start >= $1::date AND g3.interval_start <= $2::date
      AND g3.cluster_uuid::text = ANY($3::text[])
      AND g3.schedule_type = 'all_hours'
    GROUP BY g3.cluster_uuid, g3.node_name
    HAVING MAX(g3.interval_start) >= $6::timestamptz
  ) fresh ON fresh.cluster_uuid = g.cluster_uuid AND fresh.node_name = g.node_name
  WHERE g.org_id = $7
    AND g.interval_start >= $1::date AND g.interval_start <= $2::date
    AND g.cluster_uuid::text = ANY($3::text[])
    AND g.schedule_type = 'all_hours'
    AND ($4::text = '' OR LOWER(TRIM(g.node_name)) = LOWER(TRIM($4)))
    AND ($5::text = '' OR STRPOS(LOWER(g.gpu_model_name), LOWER($5)) > 0)
  GROUP BY g.cluster_uuid, g.node_name, g.gpu_model_name
) sub`
	var n int
	err := pool.QueryRow(ctx, q, startD, endD, clusterUUIDs, nodeContains, gpuContains, cutoff, orgID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count node GPU triples: %w", err)
	}
	return n, nil
}

// NodeGPUTripleSeek is the keyset cursor position for GPU triple pagination.
type NodeGPUTripleSeek struct {
	SortValue   interface{}
	ClusterUUID string
	NodeName    string
	GPUModel    string
}

// ListNodeGPUTriplesPage returns one page of distinct (cluster, node, gpu_model) keys
// after excluding nodes outside the GPU freshness window (see CountNodeGPUTriples).
// When seek is non-nil, offset is ignored and a keyset seek is applied after seek.SortValue.
func ListNodeGPUTriplesPage(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUIDs []string, start, end, now time.Time, nodeContains, gpuContains string, orderByColumn string, orderDesc bool, limit, offset int, seek *NodeGPUTripleSeek) ([]NodeGPUTriple, error) {
	if len(clusterUUIDs) == 0 {
		return nil, nil
	}
	orderSQL := gpuTripleOrderExpr(orderByColumn, orderDesc)
	sortCol := "node_name"
	switch orderByColumn {
	case "cluster_uuid":
		sortCol = "cluster_uuid"
	case "gpu_model", "gpu_model_name":
		sortCol = "gpu_model_name"
	}
	startD := start.Format("2006-01-02")
	endD := end.Format("2006-01-02")
	cutoff := now.Add(-time.Duration(CurrentGPUThresholdSettings().NodeFreshnessDays) * 24 * time.Hour)
	q := `
SELECT page_keys.cluster_uuid, page_keys.node_name, page_keys.gpu_model_name
FROM (
  SELECT g.cluster_uuid::text AS cluster_uuid, g.node_name, g.gpu_model_name
  FROM gpu_container_digests g
  INNER JOIN (
    SELECT g3.cluster_uuid, g3.node_name
    FROM gpu_container_digests g3
    WHERE g3.org_id = $7
      AND g3.interval_start >= $1::date AND g3.interval_start <= $2::date
      AND g3.cluster_uuid::text = ANY($3::text[])
      AND g3.schedule_type = 'all_hours'
    GROUP BY g3.cluster_uuid, g3.node_name
    HAVING MAX(g3.interval_start) >= $6::timestamptz
  ) fresh ON fresh.cluster_uuid = g.cluster_uuid AND fresh.node_name = g.node_name
  WHERE g.org_id = $7
    AND g.interval_start >= $1::date AND g.interval_start <= $2::date
    AND g.cluster_uuid::text = ANY($3::text[])
    AND g.schedule_type = 'all_hours'
    AND ($4::text = '' OR LOWER(TRIM(g.node_name)) = LOWER(TRIM($4)))
    AND ($5::text = '' OR STRPOS(LOWER(g.gpu_model_name), LOWER($5)) > 0)
  GROUP BY g.cluster_uuid, g.node_name, g.gpu_model_name
) page_keys`

	args := []any{startD, endD, clusterUUIDs, nodeContains, gpuContains, cutoff, orgID}
	argIdx := 8
	if seek != nil && seek.ClusterUUID != "" {
		sortOp := ">"
		if orderDesc {
			sortOp = "<"
		}
		tie := "(page_keys.cluster_uuid, page_keys.node_name, page_keys.gpu_model_name)"
		if seek.SortValue != nil {
			q += fmt.Sprintf(` WHERE ((page_keys.%s) %s $%d OR ((page_keys.%s) IS NOT DISTINCT FROM $%d AND %s > ($%d, $%d, $%d)))`,
				sortCol, sortOp, argIdx, sortCol, argIdx,
				tie, argIdx+1, argIdx+2, argIdx+3)
			args = append(args, seek.SortValue, seek.SortValue, seek.ClusterUUID, seek.NodeName, seek.GPUModel)
			argIdx += 5
		} else {
			q += fmt.Sprintf(` WHERE %s > ($%d, $%d, $%d)`, tie, argIdx, argIdx+1, argIdx+2)
			args = append(args, seek.ClusterUUID, seek.NodeName, seek.GPUModel)
			argIdx += 3
		}
	}
	q += ` ORDER BY ` + strings.ReplaceAll(orderSQL, "g.", "page_keys.")

	if limit > 0 {
		q += fmt.Sprintf(` LIMIT $%d`, argIdx)
		args = append(args, limit)
		argIdx++
		if seek == nil && offset > 0 {
			q += fmt.Sprintf(` OFFSET $%d`, argIdx)
			args = append(args, offset)
		}
	}

	rows, err := pool.Query(ctx, q, args...)
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
WHERE g.org_id = $2
  AND g.cluster_uuid::text = ANY($1::text[])
  AND g.schedule_type = 'all_hours'`
	var dc int
	if err := pool.QueryRow(ctx, qClusters, clusterUUIDs, orgID).Scan(&dc); err != nil {
		return 0, 0, fmt.Errorf("count org GPU clusters: %w", err)
	}

	qTriples := `
SELECT COUNT(*)::int FROM (
  SELECT DISTINCT g.cluster_uuid, g.node_name, g.gpu_model_name
  FROM gpu_container_digests g
  WHERE g.org_id = $2
    AND g.cluster_uuid::text = ANY($1::text[])
    AND g.schedule_type = 'all_hours'
) sub`
	var dt int
	if err := pool.QueryRow(ctx, qTriples, clusterUUIDs, orgID).Scan(&dt); err != nil {
		return 0, 0, fmt.Errorf("count org GPU triples: %w", err)
	}
	return dc, dt, nil
}
