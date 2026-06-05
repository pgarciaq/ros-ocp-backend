package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GPUMIGKey identifies a container × GPU model row for MIG list pagination.
type GPUMIGKey struct {
	ClusterUUID string
	Namespace   string
	Workload    string
	Container   string
	GPUModel    string
}

// GPUMIGKeySeek is the keyset cursor position for MIG list pagination.
type GPUMIGKeySeek struct {
	SortValue   interface{}
	ClusterUUID string
	Namespace   string
	Container   string
	GPUModel    string
}

func gpuMIGPageOrderExpr(orderBy string, desc bool) string {
	col := "page_keys.cluster_uuid::text"
	switch orderBy {
	case "namespace":
		col = "page_keys.namespace"
	case "workload":
		col = "page_keys.workload"
	case "container":
		col = "page_keys.container_name"
	case "gpu_model":
		col = "page_keys.gpu_model_name"
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	return fmt.Sprintf("%s %s NULLS LAST, page_keys.cluster_uuid::text ASC, page_keys.namespace ASC, page_keys.container_name ASC, page_keys.gpu_model_name ASC", col, dir)
}

// CountGPUMIGKeys returns distinct container × GPU model keys in the digest window.
func CountGPUMIGKeys(ctx context.Context, pool *pgxpool.Pool, clusterUUIDs []string, start, end time.Time) (int, error) {
	if len(clusterUUIDs) == 0 {
		return 0, nil
	}
	q := `
SELECT COUNT(*) FROM (
  SELECT g.cluster_uuid, g.namespace, g.workload, g.container_name, g.gpu_model_name
  FROM gpu_container_digests g
  WHERE g.interval_start >= $1::date AND g.interval_start <= $2::date
    AND g.cluster_uuid::text = ANY($3::text[])
  GROUP BY g.cluster_uuid, g.namespace, g.workload, g.container_name, g.gpu_model_name
) sub`
	var n int
	err := pool.QueryRow(ctx, q, start.Format("2006-01-02"), end.Format("2006-01-02"), clusterUUIDs).Scan(&n)
	return n, err
}

// ListGPUMIGKeysPage returns one page of container × GPU model keys for MIG listing.
func ListGPUMIGKeysPage(ctx context.Context, pool *pgxpool.Pool, clusterUUIDs []string, start, end time.Time, orderBy string, orderDesc bool, limit, offset int, seek *GPUMIGKeySeek) ([]GPUMIGKey, error) {
	if len(clusterUUIDs) == 0 {
		return nil, nil
	}
	sortCol := "page_keys.cluster_uuid::text"
	switch orderBy {
	case "namespace":
		sortCol = "page_keys.namespace"
	case "workload":
		sortCol = "page_keys.workload"
	case "container":
		sortCol = "page_keys.container_name"
	case "gpu_model":
		sortCol = "page_keys.gpu_model_name"
	}
	q := `
SELECT page_keys.cluster_uuid::text, page_keys.namespace, page_keys.workload, page_keys.container_name, page_keys.gpu_model_name
FROM (
  SELECT g.cluster_uuid, g.namespace, g.workload, g.container_name, g.gpu_model_name
  FROM gpu_container_digests g
  WHERE g.interval_start >= $1::date AND g.interval_start <= $2::date
    AND g.cluster_uuid::text = ANY($3::text[])
  GROUP BY g.cluster_uuid, g.namespace, g.workload, g.container_name, g.gpu_model_name
) page_keys`
	args := []any{start.Format("2006-01-02"), end.Format("2006-01-02"), clusterUUIDs}
	argIdx := 4
	if seek != nil && seek.ClusterUUID != "" {
		sortOp := ">"
		if orderDesc {
			sortOp = "<"
		}
		tie := "(page_keys.cluster_uuid::text, page_keys.namespace, page_keys.container_name, page_keys.gpu_model_name)"
		if seek.SortValue != nil {
			q += fmt.Sprintf(` WHERE ((%s) %s $%d OR ((%s) IS NOT DISTINCT FROM $%d AND %s > ($%d, $%d, $%d, $%d)))`,
				sortCol, sortOp, argIdx, sortCol, argIdx,
				tie, argIdx+1, argIdx+2, argIdx+3, argIdx+4)
			args = append(args, seek.SortValue, seek.SortValue, seek.ClusterUUID, seek.Namespace, seek.Container, seek.GPUModel)
			argIdx += 6
		} else {
			q += fmt.Sprintf(` WHERE %s > ($%d, $%d, $%d, $%d)`, tie, argIdx, argIdx+1, argIdx+2, argIdx+3)
			args = append(args, seek.ClusterUUID, seek.Namespace, seek.Container, seek.GPUModel)
			argIdx += 4
		}
	}
	q += ` ORDER BY ` + gpuMIGPageOrderExpr(orderBy, orderDesc)

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
		return nil, fmt.Errorf("list GPU MIG keys: %w", err)
	}
	defer rows.Close()
	var out []GPUMIGKey
	for rows.Next() {
		var k GPUMIGKey
		if err := rows.Scan(&k.ClusterUUID, &k.Namespace, &k.Workload, &k.Container, &k.GPUModel); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
