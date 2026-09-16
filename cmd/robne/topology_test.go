// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

func TestResolveManifestTopology(t *testing.T) {
	cases := []struct {
		name      string
		manifest  *libcsv.Manifest
		wantTopo  topology.ClusterTopology
		wantWrite bool
	}{
		{
			name:      "absent manifest leaves stored default untouched",
			manifest:  nil,
			wantTopo:  topology.TopologyUnknown,
			wantWrite: false,
		},
		{
			name: "management facts classify management and persist",
			manifest: &libcsv.Manifest{ClusterUUID: "c1", Topology: topology.TopologyFacts{
				ControlPlaneTopology:         "HighlyAvailable",
				HostedClusterCount:           1,
				HostedControlPlaneNamespaces: []string{"hc01-infra-hc01"},
			}},
			wantTopo:  topology.TopologyManagement,
			wantWrite: true,
		},
		{
			name:      "present-but-empty facts persist unknown, not skip",
			manifest:  &libcsv.Manifest{ClusterUUID: "c1"},
			wantTopo:  topology.TopologyUnknown,
			wantWrite: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTopo, gotWrite := resolveManifestTopology(tc.manifest)
			assert.Equal(t, tc.wantTopo, gotTopo)
			assert.Equal(t, tc.wantWrite, gotWrite)
		})
	}
}
