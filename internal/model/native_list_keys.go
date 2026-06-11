package model

import (
	"strings"

	"gorm.io/gorm"
)

var nativeRecKeysFilterAtoms = map[string]string{
	"c.cluster_uuid = ?":                    "ock.cluster_uuid = ?",
	"c.cluster_uuid != ?":                   "ock.cluster_uuid != ?",
	"c.cluster_alias ILIKE ? ESCAPE '\\'":   "c.cluster_alias ILIKE ? ESCAPE '\\'",
	"c.cluster_alias != ?":                  "c.cluster_alias != ?",
	"rs.namespace ILIKE ? ESCAPE '\\'":      "ock.namespace ILIKE ? ESCAPE '\\'",
	"rs.namespace = ?":                      "ock.namespace = ?",
	"rs.namespace != ?":                     "ock.namespace != ?",
	"rs.workload ILIKE ? ESCAPE '\\'":       "ock.workload ILIKE ? ESCAPE '\\'",
	"rs.workload = ?":                       "ock.workload = ?",
	"rs.workload != ?":                      "ock.workload != ?",
	"rs.workload_type ILIKE ? ESCAPE '\\'":  "ock.workload_type ILIKE ? ESCAPE '\\'",
	"rs.workload_type = ?":                  "ock.workload_type = ?",
	"rs.workload_type != ?":                 "ock.workload_type != ?",
	"LOWER(rs.workload_type) = ?":           "LOWER(ock.workload_type) = ?",
	"LOWER(rs.workload_type) != ?":          "LOWER(ock.workload_type) != ?",
	"rs.container_name ILIKE ? ESCAPE '\\'": "ock.container_name ILIKE ? ESCAPE '\\'",
	"rs.container_name = ?":                 "ock.container_name = ?",
	"rs.container_name != ?":                "ock.container_name != ?",
}

var nativeRecDetailOnlyQueryKeys = map[string]struct{}{
	"rs.updated_at >= ?":         {},
	"rs.updated_at < ?":          {},
	"rs.stale = ?":               {},
	"rs.has_gpu = ?":             {},
	"rs.gpu_classification IN ?": {},
	"rs.idle_state IN ?":         {},
	"rs.gpu_idle_state IN ?":     {},
	"rs.term IN ?":               {},
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
		if key == TagFiltersQueryKey {
			continue
		}
		if _, ok := nativeRecDetailOnlyQueryKeys[key]; ok {
			detailParams[key] = values
			continue
		}
		if isCompositeOfAtoms(key, nativeRecDetailOnlyFilterAtoms, []string{" OR ", " AND "}) {
			detailParams[key] = values
			continue
		}
		if isAllowedNativeRecommendationQueryKey(key) {
			keysParams[key] = values
			// Identity filters must also constrain recommendation_sets rows. The org_keys
			// leg alone is insufficient when org_container_keys collapses workload_type
			// (and cluster) across tenants of the same namespace/workload/container tuple.
			if _, isAtom := nativeRecFilterAtoms[key]; isAtom ||
				isCompositeOfAtoms(key, nativeRecFilterAtoms, []string{" OR ", " AND "}) {
				detailParams[key] = values
			}
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
