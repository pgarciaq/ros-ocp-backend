// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	roscsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

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
// namespace. Namespace alone decides: pod/workload names never include or
// exclude, and blank owner joins do not disqualify. A nil set excludes all.
func IncludeRow(row roscsv.Row, hcpNamespaces map[string]bool) bool {
	return hcpNamespaces[row.Namespace]
}
