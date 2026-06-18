package api

import (
	"context"
	"encoding/json"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// enrichContainerTags loads resolved_tags from org_container_keys for the given
// page of results and attaches them to each NativeContainerResult.
func enrichContainerTags(ctx context.Context, orgID string, results []model.NativeContainerResult) {
	if len(results) == 0 || !config.TagsFeatureEnabled() {
		return
	}
	pool := db.GetPool()
	if pool == nil {
		return
	}

	type nsKey struct {
		ClusterUUID string
		Namespace   string
	}
	seen := make(map[nsKey]struct{}, len(results))
	keys := make([]nsKey, 0, len(results))
	for _, r := range results {
		k := nsKey{r.ClusterUUID, r.Project}
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}

	clusterUUIDs := make([]string, 0, len(keys))
	namespaces := make([]string, 0, len(keys))
	for _, k := range keys {
		clusterUUIDs = append(clusterUUIDs, k.ClusterUUID)
		namespaces = append(namespaces, k.Namespace)
	}

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (cluster_uuid, namespace)
			cluster_uuid, namespace, resolved_tags
		FROM org_container_keys
		WHERE org_id = $1
		  AND (cluster_uuid, namespace) IN (
			SELECT unnest($2::uuid[]), unnest($3::text[])
		  )
		ORDER BY cluster_uuid, namespace`,
		orgID, clusterUUIDs, namespaces,
	)
	if err != nil {
		log.Warnf("enrichContainerTags: query failed for org %s: %v", orgID, err)
		return
	}
	defer rows.Close()

	type tagsEntry struct {
		Tags map[string]string
	}
	tagMap := make(map[nsKey]map[string]string, len(keys))
	for rows.Next() {
		var clusterUUID, namespace string
		var tagsJSON []byte
		if err := rows.Scan(&clusterUUID, &namespace, &tagsJSON); err != nil {
			log.Warnf("enrichContainerTags: scan error: %v", err)
			continue
		}
		if len(tagsJSON) > 2 {
			var tags map[string]string
			if err := json.Unmarshal(tagsJSON, &tags); err != nil {
				log.Warnf("enrichContainerTags: unmarshal tags for %s/%s: %v", clusterUUID, namespace, err)
				continue
			}
			if len(tags) > 0 {
				tagMap[nsKey{clusterUUID, namespace}] = tags
			}
		}
	}
	if err := rows.Err(); err != nil {
		log.Warnf("enrichContainerTags: rows iteration error: %v", err)
	}

	for i := range results {
		k := nsKey{results[i].ClusterUUID, results[i].Project}
		if tags, ok := tagMap[k]; ok {
			results[i].Tags = tags
		}
	}
}
