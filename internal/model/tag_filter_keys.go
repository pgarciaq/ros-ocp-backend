package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
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
			if len(f.Values) == 1 {
				payload, err := json.Marshal(map[string]string{f.Key: f.Values[0]})
				if err != nil {
					continue
				}
				query += fmt.Sprintf(" AND ock.resolved_tags @> $%d::jsonb", argIdx)
				args = append(args, string(payload))
				argIdx++
				continue
			}
			placeholders := make([]string, len(f.Values))
			query += fmt.Sprintf(" AND ock.resolved_tags->>$%d IN (", argIdx)
			args = append(args, f.Key)
			argIdx++
			for i, v := range f.Values {
				placeholders[i] = fmt.Sprintf("$%d", argIdx)
				args = append(args, v)
				argIdx++
			}
			query += strings.Join(placeholders, ", ") + ")"
		}
	} else {
		schema, err := tags.TenantSchema(orgID)
		if err != nil {
			return nil, err
		}
		tagValuesTable := pgx.Identifier{schema, "reporting_ocptags_values"}.Sanitize()
		for _, f := range filters {
			if f.Key == "" || len(f.Values) == 0 {
				continue
			}
			var matchClause string
			if len(f.Values) == 1 && f.Values[0] == "*" {
				matchClause = fmt.Sprintf("tv.key = $%d", argIdx)
				args = append(args, f.Key)
				argIdx++
			} else {
				placeholders := make([]string, len(f.Values))
				matchClause = fmt.Sprintf("tv.key = $%d AND tv.value IN (", argIdx)
				args = append(args, f.Key)
				argIdx++
				for i, v := range f.Values {
					placeholders[i] = fmt.Sprintf("$%d", argIdx)
					args = append(args, v)
					argIdx++
				}
				matchClause += strings.Join(placeholders, ", ") + ")"
			}
			query += fmt.Sprintf(` AND EXISTS (
				SELECT 1 FROM %s tv,
				     unnest(tv.cluster_ids, tv.namespaces) AS t(cluster_id, namespace)
				WHERE %s
				  AND t.cluster_id = ock.cluster_uuid::text
				  AND t.namespace = ock.namespace
			)`, tagValuesTable, matchClause)
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
