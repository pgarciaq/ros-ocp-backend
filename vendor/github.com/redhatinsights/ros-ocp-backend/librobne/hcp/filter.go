// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import ()

// NewNamespaceSet builds the known-HCP-namespace lookup from the fact source
// (TopologyFacts.HostedControlPlaneNamespaces). Membership is exact: shaping
// like {hc.namespace}-{hc.name} without membership excludes.
func NewNamespaceSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		if name != "" {
			set[name] = true
		}
	}
	return set
}

// IncludeRow reports whether a ROS container row belongs to a known HCP
// namespace, keyed by namespace. Namespace alone decides: pod/workload
// names never include or exclude, and blank owner joins do not
// disqualify. A nil set excludes all.
//
// (Cycle-break note, #700 Task 1: this package must not import
// librobne/csv — csv now imports hcp for the capacity contract constants,
// and the old csv.Row-keyed signatures closed an import cycle. The
// namespace-string form is the single signature; GuardrailFor below is
// the historical name kept for the engine call sites.)
func IncludeRow(namespace string, hcpNamespaces map[string]bool) bool {
	return hcpNamespaces[namespace]
}

// GuardrailFor is the string-keyed form of the namespace rule for callers
// working with digest rows (engine path). Same rule: exact membership,
// names never decide, nil set excludes all.
func GuardrailFor(namespace string, hcpNamespaces map[string]bool) bool {
	return hcpNamespaces[namespace]
}
