package model

import (
	"strings"

	"gorm.io/gorm"
)

var nativeRecKeysFilterAtoms = map[string]string{
	"c.cluster_uuid = ?":        "ock.cluster_uuid = ?",
	"c.cluster_uuid != ?":       "ock.cluster_uuid != ?",
	"c.cluster_alias ILIKE ?":   "c.cluster_alias ILIKE ?",
	"c.cluster_alias != ?":      "c.cluster_alias != ?",
	"rs.namespace ILIKE ?":      "ock.namespace ILIKE ?",
	"rs.namespace = ?":          "ock.namespace = ?",
	"rs.namespace != ?":         "ock.namespace != ?",
	"rs.workload ILIKE ?":       "ock.workload ILIKE ?",
	"rs.workload = ?":           "ock.workload = ?",
	"rs.workload != ?":          "ock.workload != ?",
	"rs.workload_type ILIKE ?":  "ock.workload_type ILIKE ?",
	"rs.workload_type = ?":      "ock.workload_type = ?",
	"rs.workload_type != ?":     "ock.workload_type != ?",
	"rs.container_name ILIKE ?": "ock.container_name ILIKE ?",
	"rs.container_name = ?":     "ock.container_name = ?",
	"rs.container_name != ?":    "ock.container_name != ?",
}

var nativeRecDetailOnlyQueryKeys = map[string]struct{}{
	"rs.updated_at >= ?":         {},
	"rs.updated_at < ?":          {},
	"rs.stale = ?":               {},
	"rs.has_gpu = ?":             {},
	"rs.gpu_classification IN ?": {},
	"rs.idle_state IN ?":         {},
	"rs.gpu_idle_state IN ?":     {},
}

func usesOrgContainerKeys(queryParams map[string]interface{}) bool {
	stale, ok := queryParams["rs.stale = ?"]
	if !ok {
		return false
	}
	b, ok := stale.(bool)
	return ok && !b
}

func splitNativeListQueryParams(queryParams map[string]interface{}) (keysParams, detailParams map[string]interface{}) {
	keysParams = make(map[string]interface{})
	detailParams = make(map[string]interface{})
	for key, values := range queryParams {
		if _, ok := nativeRecDetailOnlyQueryKeys[key]; ok {
			detailParams[key] = values
			continue
		}
		if isAllowedNativeRecommendationQueryKey(key) {
			keysParams[key] = values
		}
	}
	return keysParams, detailParams
}

func remapNativeKeysQueryKey(key string) (string, bool) {
	if mapped, ok := nativeRecKeysFilterAtoms[key]; ok {
		return mapped, true
	}
	return remapCompositeNativeKeysQueryKey(key)
}

func remapCompositeNativeKeysQueryKey(key string) (string, bool) {
	k := strings.TrimSpace(key)
	if k == "" {
		return "", false
	}
	for len(k) >= 2 && k[0] == '(' && k[len(k)-1] == ')' {
		k = strings.TrimSpace(k[1 : len(k)-1])
	}
	for _, sep := range []string{" OR ", " AND "} {
		chunks := splitAtTopLevelSep(k, sep)
		if len(chunks) > 1 {
			remapped := make([]string, 0, len(chunks))
			for _, chunk := range chunks {
				mappedChunk, ok := remapNativeKeysQueryKey(chunk)
				if !ok {
					return "", false
				}
				remapped = append(remapped, mappedChunk)
			}
			return strings.Join(remapped, sep), true
		}
	}
	mapped, ok := nativeRecKeysFilterAtoms[k]
	return mapped, ok
}

// ApplyQueryParamsToKeys adds key-table WHERE clauses from parsed query parameters.
func ApplyQueryParamsToKeys(query *gorm.DB, queryParams map[string]interface{}) *gorm.DB {
	for key, values := range queryParams {
		mappedKey, ok := remapNativeKeysQueryKey(key)
		if !ok {
			log.Warnf("ApplyQueryParamsToKeys: skipping unknown query key %q", key)
			continue
		}
		query = query.Where(mappedKey, values)
	}
	return query
}
