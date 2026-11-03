package rbac

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// OpenShiftDataAccessEligible reports whether the user holds any affirmative
// OpenShift data-read grant, or a pass-through wildcard.
//
// It mirrors Koku's permission lattice (koku/koku/rbac.py): resources start
// EMPTY — an ABSENT OpenShift grant is not unrestricted access (#753). A
// settings-only role (settings.read / settings.write) is therefore NOT
// data-read eligible.
//
// Semantics:
//   - RBAC disabled              → true (dev/test mode: nothing restricts)
//   - global "*" wildcard        → true
//   - an openshift.* key with a NON-EMPTY list → true. The grant must be
//     affirmative: an explicitly EMPTY list (producible by the middleware
//     from resource definitions that yield no values) is NOT eligibility —
//     Koku treats an empty grant as no access, and the container/namespace
//     builders do not filter on node grants, so an empty node grant must
//     not authorize them (#753 review).
//   - otherwise                  → false (data scope is empty)
//
// Absent CHILD grants (e.g. no openshift.node under an affirmative
// openshift.cluster) still inherit the parent's scope — that inheritance is
// each data helper's existing behavior and is intentionally unchanged.
func OpenShiftDataAccessEligible(userPerms map[string][]string) bool {
	if !config.GetConfig().RBACEnabled {
		return true
	}
	if _, ok := userPerms["*"]; ok {
		return true
	}
	for _, key := range []string{"openshift.cluster", "openshift.node", "openshift.project"} {
		if v, ok := userPerms[key]; ok && len(v) > 0 {
			return true
		}
	}
	return false
}
