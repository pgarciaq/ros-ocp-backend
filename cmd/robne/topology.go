// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

// resolveManifestTopology classifies the loaded operator manifest for W0
// persistence (#407). Absent manifest (single-CSV inputs) reports write=false
// so the stored default stays untouched; a present manifest — even with empty
// facts from pre-#406 payloads — classifies (unknown) and persists.
func resolveManifestTopology(m *libcsv.Manifest) (topology.ClusterTopology, bool) {
	if m == nil {
		return topology.TopologyUnknown, false
	}
	return topology.Classify(m.Topology), true
}
