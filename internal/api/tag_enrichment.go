package api

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

type namespaceTagKey struct {
	ClusterUUID string
	Namespace   string
}

func loadNamespaceTagsMap(ctx context.Context, orgID string, keys []namespaceTagKey) map[namespaceTagKey]map[string]string {
	tagMap := make(map[namespaceTagKey]map[string]string, len(keys))
	if len(keys) == 0 || !config.TagsFeatureEnabled() {
		return tagMap
	}
	pool := db.GetPool()
	if pool == nil {
		return tagMap
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
		log.Warnf("loadNamespaceTagsMap: query failed for org %s: %v", orgID, err)
		return tagMap
	}
	defer rows.Close()

	for rows.Next() {
		var clusterUUID, namespace string
		var tagsJSON []byte
		if err := rows.Scan(&clusterUUID, &namespace, &tagsJSON); err != nil {
			log.Warnf("loadNamespaceTagsMap: scan error: %v", err)
			continue
		}
		if len(tagsJSON) > 2 {
			tags, err := projectStoredTags(tagsJSON)
			if err != nil {
				log.Warnf("loadNamespaceTagsMap: unmarshal tags for %s/%s: %v", clusterUUID, namespace, err)
				continue
			}
			if len(tags) > 0 {
				tagMap[namespaceTagKey{clusterUUID, namespace}] = tags
			}
		}
	}
	if err := rows.Err(); err != nil {
		log.Warnf("loadNamespaceTagsMap: rows iteration error: %v", err)
	}
	return tagMap
}

// projectStoredTags unmarshals stored resolved_tags tolerantly (#814 D4:
// values may be arrays — the post-#814 storage — or scalars — the pre-#814
// legacy rows, until each org's next push) and projects each value to a
// single string (D5 phase 1): a scalar string maps to itself; an array maps
// to its alphabetically-first element (the write path stores sorted-unique
// arrays; sorted defensively so the projection does not depend on write
// discipline). Keys holding other JSON types are skipped. The response shape
// stays map[string]string — unchanged for koku-ui and IQE.
func projectStoredTags(tagsJSON []byte) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(tagsJSON, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for key, val := range raw {
		var single string
		if err := json.Unmarshal(val, &single); err == nil {
			out[key] = single
			continue
		}
		var multi []string
		if err := json.Unmarshal(val, &multi); err == nil && len(multi) > 0 {
			sorted := append([]string(nil), multi...)
			sort.Strings(sorted)
			out[key] = sorted[0]
			continue
		}
		log.Warnf("projectStoredTags: skipping non-string tag value for key %q", key)
	}
	return out, nil
}

func uniqueNamespaceTagKeys(keys []namespaceTagKey) []namespaceTagKey {
	seen := make(map[namespaceTagKey]struct{}, len(keys))
	out := make([]namespaceTagKey, 0, len(keys))
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

// enrichContainerTags loads resolved_tags from org_container_keys for the given
// page of results and attaches them to each NativeContainerResult.
func enrichContainerTags(ctx context.Context, orgID string, results []model.NativeContainerResult) {
	if len(results) == 0 {
		return
	}
	keys := make([]namespaceTagKey, 0, len(results))
	for _, r := range results {
		keys = append(keys, namespaceTagKey{r.ClusterUUID, r.Project})
	}
	tagMap := loadNamespaceTagsMap(ctx, orgID, uniqueNamespaceTagKeys(keys))
	for i := range results {
		k := namespaceTagKey{results[i].ClusterUUID, results[i].Project}
		if tags, ok := tagMap[k]; ok {
			results[i].Tags = tags
		}
	}
}

// enrichNamespaceTags loads resolved_tags from org_container_keys for namespace list/detail rows.
func enrichNamespaceTags(ctx context.Context, orgID string, results []model.NativeNamespaceResult) {
	if len(results) == 0 {
		return
	}
	keys := make([]namespaceTagKey, 0, len(results))
	for _, r := range results {
		keys = append(keys, namespaceTagKey{r.ClusterUUID, r.Project})
	}
	tagMap := loadNamespaceTagsMap(ctx, orgID, uniqueNamespaceTagKeys(keys))
	for i := range results {
		k := namespaceTagKey{results[i].ClusterUUID, results[i].Project}
		if tags, ok := tagMap[k]; ok {
			results[i].Tags = tags
		}
	}
}
