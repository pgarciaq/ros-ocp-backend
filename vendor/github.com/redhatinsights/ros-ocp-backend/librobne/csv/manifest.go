// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package csv

import (
	"encoding/json"

	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

// Manifest is the operator tarball manifest.json: cluster identity plus
// resource-optimization topology facts (W0, #406/#407). Nil when the input
// carries no manifest (single CSV). Pre-#406 manifests parse with empty
// facts (classify unknown); corrupt manifests are ignored so previously
// working loads never newly fail on an auxiliary file.
type Manifest struct {
	ClusterUUID string
	Topology    topology.TopologyFacts
}

type manifestFile struct {
	ClusterID string `json:"cluster_id"`
	CRStatus  struct {
		Topology topology.TopologyFacts `json:"topology"`
	} `json:"cr_status"`
}

// parseManifest parses manifest.json bytes. It returns nil for corrupt input
// by design: the manifest is auxiliary to the CSV rows.
func parseManifest(data []byte) *Manifest {
	var mf manifestFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil
	}
	return &Manifest{ClusterUUID: mf.ClusterID, Topology: mf.CRStatus.Topology}
}
