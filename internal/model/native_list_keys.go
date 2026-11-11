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
	"rs.stale = ?":                          "ock.is_stale = ?",
}

var nativeRecDetailOnlyQueryKeys = map[string]struct{}{
	"rs.updated_at >= ?":         {},
	"rs.updated_at < ?":          {},
	"rs.has_gpu = ?":             {},
	"rs.gpu_classification IN ?": {},
	"rs.idle_state IN ?":         {},
	"rs.gpu_idle_state IN ?":     {},
	"rs.term IN ?":               {},
	// #841 (branch review): accepted-but-dropped filters — these are
	// recommendation-dependent predicates and were classified into NEITHER
	// leg, so the keys remapper logged "skipping unknown query key" and
	// the filter silently disappeared. Detail-only classification puts
	// them under #842's eligibility machinery.
	"rs.engine IN ?":   {},
	"rs.category IN ?": {},
}

func usesOrgContainerKeys(_ map[string]interface{}) bool {
	return true
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
			// Stale filter must also constrain the detail join so that the
			// recommendation_sets rows matched are consistent with the keys page.
			if key == "rs.stale = ?" {
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
		query = query.Where(mappedKey, bindQueryParamValues(mappedKey, values)...)
	}
	return query
}

// applyNativeContainerDetailEligibility gates key-table rows on the
// EXISTENCE of a matching recommendation row carrying the detail-only
// predicates (#842, branch review finding 3): counts and page selection
// must see the same eligibility the detail query enforces, or a page of
// nonmatching keys swallows the limit and HasNext reports no continuation
// while matches remain beyond the selected keys. The identity shape
// mirrors nativeContainerRSJoin. No-op when no detail predicates exist —
// the common unfiltered path pays nothing.
func applyNativeContainerDetailEligibility(query *gorm.DB, detailParams map[string]interface{}) *gorm.DB {
	if len(detailParams) == 0 {
		return query
	}
	prefix := "EXISTS (SELECT 1 FROM recommendation_sets rs WHERE rs.org_id = ock.org_id" +
		" AND rs.cluster_uuid = ock.cluster_uuid AND rs.namespace = ock.namespace" +
		" AND rs.workload = ock.workload AND rs.workload_type = ock.workload_type" +
		" AND rs.container_name = ock.container_name"
	// ONE EXISTS containing ALL detail predicates (#841 review F1):
	// per-predicate EXISTSes could match DIFFERENT rows of the same
	// identity — (engine=cost, category=zombie) and (engine=performance,
	// category=optimized) satisfy engine=performance AND category=zombie
	// across two rows while no single row does — counting and paging the
	// identity but returning no detail. A single EXISTS requires the
	// conjunction on ONE row, exactly what the detail query enforces.
	// Each predicate is PARENTHESIZED: an unparenthesized OR would split
	// the AND and let the second disjunct escape the identity scope
	// (#842 review — a tenant-scope bypass). rs.stale is EXCLUDED — the
	// keys leg already filters ock.is_stale; excluding it keeps the
	// default request from doubling that check.
	//
	// Binding: predicates concatenate with their values in matching
	// order — the distributor's per-key output is flattened into the
	// combined argument list.
	var sb strings.Builder
	sb.WriteString(prefix)
	var args []interface{}
	for key, values := range detailParams {
		if key == "rs.stale = ?" {
			continue
		}
		sb.WriteString(" AND (" + key + ")")
		args = append(args, bindQueryParamValues(key, values)...)
	}
	if len(args) == 0 {
		return query
	}
	if len(args) == 0 {
		return query
	}
	if len(args) == 0 {
		return query
	}
	return query.Where(sb.String()+")", args...)
}
