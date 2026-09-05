package gpu

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/model/types"
)

// GPUMIGListFilters holds optional SQL filters for the gpu_mig_recommendation_sets list.
type GPUMIGListFilters struct {
	ClusterUUIDs  []string
	Namespaces    []string
	Workloads     []string
	Term          string
	GPUIdleStates []string
	// TagFilterFunc, if non-nil, is called with the current argIdx and appends tag-filter
	// SQL and args. The returned string is AND-joined to the WHERE clause.
	TagFilterFunc func(argIdx int) (clause string, args []any, nextArgIdx int)
}

func gpuMIGListSortColumn(orderBy string) string {
	switch orderBy {
	case "namespace":
		return "m.namespace"
	case "workload":
		return "m.workload"
	case "container":
		return "m.container_name"
	case "gpu_model":
		return "m.gpu_model_name"
	case "term":
		return "m.term"
	case "confidence":
		return "m.confidence"
	case "gpu_idle_state":
		return "m.gpu_idle_state"
	default:
		return "m.cluster_uuid::text"
	}
}

func gpuMIGListTiebreaker() string {
	return "m.cluster_uuid::text, m.namespace, m.container_name, m.gpu_model_name, m.term"
}

// CountGPUMIGRecommendationSets returns COUNT(*) from gpu_mig_recommendation_sets matching the filters.
func CountGPUMIGRecommendationSets(ctx context.Context, pool *pgxpool.Pool, orgID string, filters GPUMIGListFilters) (int, error) {
	q := `SELECT COUNT(*) FROM gpu_mig_recommendation_sets m WHERE m.org_id = $1`
	args := []any{orgID}
	argIdx := 2

	q, args, argIdx = appendGPUMIGFilters(q, args, argIdx, filters)

	var n int
	err := pool.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}

// GPUMIGListSeek is the keyset cursor for paginating gpu_mig_recommendation_sets.
type GPUMIGListSeek struct {
	SortValue   interface{}
	ClusterUUID string
	Namespace   string
	Container   string
	GPUModel    string
	Term        string
}

// ListGPUMIGRecommendationSets returns one page of rows from gpu_mig_recommendation_sets.
func ListGPUMIGRecommendationSets(
	ctx context.Context, pool *pgxpool.Pool,
	orgID string,
	filters GPUMIGListFilters,
	orderBy string, orderDesc bool,
	limit, offset int,
	seek *GPUMIGListSeek,
) ([]types.GPUMIGRecommendationSetRow, error) {
	sortCol := gpuMIGListSortColumn(orderBy)
	tieBreaker := gpuMIGListTiebreaker()

	// clusters is joined only for cluster_alias, predicated on c.org_id so a
	// colliding UUID cannot pick another tenant's alias (#525; same as #445 slice B).
	// Org scoping stays on m.org_id ($1, already the first arg).
	q := `SELECT
		m.cluster_uuid::text, COALESCE(c.cluster_alias, ''),
		m.namespace, m.workload, m.workload_type,
		m.container_name, m.node_name, m.gpu_model_name,
		m.term, m.recommended_gpu_profile, m.current_gpu_profile,
		m.gpu_classification, m.confidence, m.fb_usage_max_mib,
		m.total_fb_mib, m.gpu_idle_state
	FROM gpu_mig_recommendation_sets m
	LEFT JOIN clusters c ON c.cluster_uuid = m.cluster_uuid AND c.org_id = $1
	WHERE m.org_id = $1`

	args := []any{orgID}
	argIdx := 2

	q, args, argIdx = appendGPUMIGFilters(q, args, argIdx, filters)

	if seek != nil && seek.ClusterUUID != "" {
		tie := fmt.Sprintf("(%s)", tieBreaker)
		if seek.SortValue != nil {
			op := ">"
			if orderDesc {
				op = "<"
			}
			q += fmt.Sprintf(` AND ((%s %s $%d) OR ((%s) IS NOT DISTINCT FROM $%d AND %s > ($%d::text, $%d::text, $%d::text, $%d::text, $%d::text)))`,
				sortCol, op, argIdx,
				sortCol, argIdx,
				tie, argIdx+1, argIdx+2, argIdx+3, argIdx+4, argIdx+5,
			)
			args = append(args, seek.SortValue, seek.ClusterUUID, seek.Namespace, seek.Container, seek.GPUModel, seek.Term)
			argIdx += 6
		} else {
			q += fmt.Sprintf(` AND %s > ($%d::text, $%d::text, $%d::text, $%d::text, $%d::text)`,
				tie, argIdx, argIdx+1, argIdx+2, argIdx+3, argIdx+4,
			)
			args = append(args, seek.ClusterUUID, seek.Namespace, seek.Container, seek.GPUModel, seek.Term)
			argIdx += 5
		}
	}

	dir := "ASC"
	if orderDesc {
		dir = "DESC"
	}
	q += fmt.Sprintf(` ORDER BY %s %s NULLS LAST, %s`, sortCol, dir, tieBreaker)

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
		return nil, fmt.Errorf("ListGPUMIGRecommendationSets: %w", err)
	}
	defer rows.Close()

	var out []types.GPUMIGRecommendationSetRow
	for rows.Next() {
		var r types.GPUMIGRecommendationSetRow
		if err := rows.Scan(
			&r.ClusterUUID, &r.ClusterAlias,
			&r.Namespace, &r.Workload, &r.WorkloadType,
			&r.Container, &r.NodeName, &r.GPUModel,
			&r.Term, &r.RecommendedGPUProfile, &r.CurrentGPUProfile,
			&r.Classification, &r.Confidence, &r.FBUsageMaxMiB,
			&r.TotalFBMiB, &r.GPUIdleState,
		); err != nil {
			return nil, fmt.Errorf("ListGPUMIGRecommendationSets scan: %w", err)
		}
		r.ConfidenceLevel = r.Confidence
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountGPUMIGGrouped returns grouped counts from gpu_mig_recommendation_sets.
func CountGPUMIGGrouped(ctx context.Context, pool *pgxpool.Pool, orgID string, filters GPUMIGListFilters, groupByCluster bool) (int, error) {
	groupCol := pgx.Identifier{"m", "namespace"}.Sanitize()
	if groupByCluster {
		groupCol = pgx.Identifier{"m", "cluster_uuid"}.Sanitize()
	}
	q := fmt.Sprintf(`SELECT COUNT(DISTINCT %s) FROM gpu_mig_recommendation_sets m WHERE m.org_id = $1`, groupCol)
	args := []any{orgID}
	argIdx := 2
	q, args, _ = appendGPUMIGFilters(q, args, argIdx, filters)

	var n int
	err := pool.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}

// ListGPUMIGGrouped returns paginated grouped rows from gpu_mig_recommendation_sets.
func ListGPUMIGGrouped(ctx context.Context, pool *pgxpool.Pool, orgID string, filters GPUMIGListFilters, groupByCluster bool, limit, offset int) ([]types.GPUMIGGroupedRow, error) {
	groupCol := pgx.Identifier{"m", "namespace"}.Sanitize()
	label := pgx.Identifier{"namespace"}.Sanitize()
	if groupByCluster {
		groupCol = pgx.Identifier{"m", "cluster_uuid"}.Sanitize() + "::text"
		label = pgx.Identifier{"cluster_uuid"}.Sanitize()
	}

	q := fmt.Sprintf(`SELECT %s AS %s, COUNT(*) AS row_count
		FROM gpu_mig_recommendation_sets m
		WHERE m.org_id = $1`, groupCol, label)
	args := []any{orgID}
	argIdx := 2
	q, args, argIdx = appendGPUMIGFilters(q, args, argIdx, filters)
	q += fmt.Sprintf(` GROUP BY %s ORDER BY %s ASC`, groupCol, groupCol)

	pageLimit := limit + 1
	q += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, pageLimit, offset)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListGPUMIGGrouped: %w", err)
	}
	defer rows.Close()

	var out []types.GPUMIGGroupedRow
	for rows.Next() {
		var r types.GPUMIGGroupedRow
		if err := rows.Scan(&r.GroupKey, &r.Count); err != nil {
			return nil, fmt.Errorf("ListGPUMIGGrouped scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func appendGPUMIGFilters(q string, args []any, argIdx int, f GPUMIGListFilters) (string, []any, int) {
	if len(f.ClusterUUIDs) > 0 {
		q += fmt.Sprintf(` AND m.cluster_uuid::text = ANY($%d::text[])`, argIdx)
		args = append(args, f.ClusterUUIDs)
		argIdx++
	}
	if len(f.Namespaces) > 0 {
		q += fmt.Sprintf(` AND m.namespace = ANY($%d::text[])`, argIdx)
		args = append(args, f.Namespaces)
		argIdx++
	}
	if len(f.Workloads) > 0 {
		q += fmt.Sprintf(` AND m.workload = ANY($%d::text[])`, argIdx)
		args = append(args, f.Workloads)
		argIdx++
	}
	if f.Term != "" {
		q += fmt.Sprintf(` AND m.term = $%d`, argIdx)
		args = append(args, strings.ToLower(f.Term))
		argIdx++
	}
	if len(f.GPUIdleStates) > 0 {
		q += fmt.Sprintf(` AND m.gpu_idle_state = ANY($%d::text[])`, argIdx)
		args = append(args, f.GPUIdleStates)
		argIdx++
	}
	if f.TagFilterFunc != nil {
		clause, tagArgs, nextIdx := f.TagFilterFunc(argIdx)
		if clause != "" {
			q += " AND " + clause
			args = append(args, tagArgs...)
			argIdx = nextIdx
		}
	}
	return q, args, argIdx
}
