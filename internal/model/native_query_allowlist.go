package model

import "strings"

// Keys produced by MapNativeQueryParameters (handlers.go) for recommendation_sets / rs alias queries,
// plus quality (q.*) and history (h.*) date-range and filter keys that also pass through ApplyQueryParams.
var nativeRecFixedQueryKeys = map[string]struct{}{
	"rs.updated_at >= ?":        {},
	"rs.updated_at < ?":         {},
	"rs.stale = ?":              {},
	"rs.has_gpu = ?":            {},
	"rs.gpu_classification IN ?": {},

	// recommendation_quality / recommendation_history cluster filter (handlers_quality.go, handlers_history.go)
	"c.cluster_alias IN ?": {},

	// recommendation_quality (handlers_quality.go)
	"q.measured_at >= ?":    {},
	"q.measured_at < ?":     {},
	"q.namespace IN ?":      {},
	"q.workload IN ?":       {},
	"q.container_name IN ?": {},

	// recommendation_history (handlers_history.go)
	"h.recorded_at >= ?":    {},
	"h.recorded_at < ?":     {},
	"h.namespace IN ?":      {},
	"h.workload IN ?":       {},
	"h.container_name IN ?": {},
	"h.term IN ?":           {},
	"h.engine IN ?":         {},
}

// Filter fragments from buildNativeModeClause for native container listings (c.* / rs.* columns).
var nativeRecFilterAtoms = map[string]struct{}{
	"c.cluster_uuid = ?":         {},
	"c.cluster_uuid != ?":        {},
	"c.cluster_alias ILIKE ?":    {},
	"c.cluster_alias != ?":       {},
	"rs.namespace ILIKE ?":       {},
	"rs.namespace = ?":           {},
	"rs.namespace != ?":          {},
	"rs.workload ILIKE ?":        {},
	"rs.workload = ?":            {},
	"rs.workload != ?":           {},
	"rs.workload_type ILIKE ?":   {},
	"rs.workload_type = ?":       {},
	"rs.workload_type != ?":      {},
	"rs.container_name ILIKE ?":  {},
	"rs.container_name = ?":      {},
	"rs.container_name != ?":     {},
	"rs.gpu_model_name ILIKE ?":  {},
}

// Keys from MapNativeNamespaceQueryParameters for namespace_recommendation_sets (ns alias).
var nativeNSFixedQueryKeys = map[string]struct{}{
	"ns.monitoring_end_time >= ?": {},
	"ns.monitoring_end_time < ?":  {},
}

var nativeNSFilterAtoms = map[string]struct{}{
	"c.cluster_uuid = ?":      {},
	"c.cluster_uuid != ?":     {},
	"c.cluster_alias ILIKE ?": {},
	"c.cluster_alias != ?":    {},
	"ns.namespace_name ILIKE ?": {},
	"ns.namespace_name = ?":     {},
	"ns.namespace_name != ?":  {},
}

func splitAtTopLevelSep(s, sep string) []string {
	if s == "" {
		return nil
	}
	depth := 0
	start := 0
	var parts []string
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

func isCompositeOfAtoms(key string, atoms map[string]struct{}, seps []string) bool {
	k := strings.TrimSpace(key)
	if k == "" {
		return false
	}
	for len(k) >= 2 && k[0] == '(' && k[len(k)-1] == ')' {
		k = strings.TrimSpace(k[1 : len(k)-1])
	}
	for _, sep := range seps {
		chunks := splitAtTopLevelSep(k, sep)
		if len(chunks) > 1 {
			for _, c := range chunks {
				if !isCompositeOfAtoms(c, atoms, seps) {
					return false
				}
			}
			return true
		}
	}
	_, ok := atoms[k]
	return ok
}

func isAllowedNativeRecommendationQueryKey(key string) bool {
	if _, ok := nativeRecFixedQueryKeys[key]; ok {
		return true
	}
	return isCompositeOfAtoms(key, nativeRecFilterAtoms, []string{" OR ", " AND "})
}

func isAllowedNativeNamespaceQueryKey(key string) bool {
	if _, ok := nativeNSFixedQueryKeys[key]; ok {
		return true
	}
	return isCompositeOfAtoms(key, nativeNSFilterAtoms, []string{" OR ", " AND "})
}
