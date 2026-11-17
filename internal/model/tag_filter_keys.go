package model

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

// ContainerKeySet is a set of container identity tuples for tag-filtered list narrowing.
type ContainerKeySet map[string]struct{}

func containerKey(clusterUUID, namespace, workload, container string) string {
	return clusterUUID + "\x00" + namespace + "\x00" + workload + "\x00" + container
}

// MatchingContainerKeys returns container keys that satisfy all tag filters (AND across keys).
func MatchingContainerKeys(ctx context.Context, pool *pgxpool.Pool, orgID string, filters []TagFilter) (ContainerKeySet, error) {
	if len(filters) == 0 || !config.TagsFeatureEnabled() {
		return nil, nil
	}

	query := `SELECT DISTINCT ock.cluster_uuid::text, ock.namespace, ock.workload, ock.container_name
		FROM org_container_keys ock WHERE ock.org_id = $1`
	args := []interface{}{orgID}
	argIdx := 2

	if config.TagsSource() == "api" {
		for _, f := range filters {
			if f.Key == "" || len(f.Values) == 0 {
				continue
			}
			if len(f.Values) == 1 && f.Values[0] == "*" {
				query += fmt.Sprintf(" AND ock.resolved_tags ? $%d", argIdx)
				args = append(args, f.Key)
				argIdx++
				continue
			}
			pred, predArgs, n := tagValueContainmentPgx("ock.resolved_tags", f, argIdx)
			if n == 0 {
				continue
			}
			query += " AND " + pred
			args = append(args, predArgs...)
			argIdx += n
		}
	} else {
		schema, err := tags.TenantSchema(orgID)
		if err != nil {
			return nil, err
		}
		for _, f := range filters {
			if f.Key == "" || len(f.Values) == 0 {
				continue
			}
			// Same source, window and key-enabled check as every other db-mode
			// filter site (#815). The catalog's cluster/namespace arrays are
			// rebuilt over the whole reporting window, so unnest() pairs a
			// cluster with a namespace it may not own (#867).
			b := newPgxTagFragmentBuilder(argIdx)
			query += " AND " + b.labelSummaryTagMatch(schema, "ock.cluster_uuid::text", "ock.namespace", f)
			args = append(args, b.args...)
			argIdx += len(b.args)
		}
	}

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(ContainerKeySet)
	for rows.Next() {
		var clusterUUID, namespace, workload, container string
		if err := rows.Scan(&clusterUUID, &namespace, &workload, &container); err != nil {
			return nil, err
		}
		out[containerKey(clusterUUID, namespace, workload, container)] = struct{}{}
	}
	return out, rows.Err()
}

// Contains reports whether the container key is in the set (nil set matches all).
func (s ContainerKeySet) Contains(clusterUUID, namespace, workload, container string) bool {
	if s == nil {
		return true
	}
	_, ok := s[containerKey(clusterUUID, namespace, workload, container)]
	return ok
}
