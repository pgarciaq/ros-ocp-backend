// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name  string
		facts TopologyFacts
		want  ClusterTopology
	}{
		{
			name: "management compact with hosted cluster",
			facts: TopologyFacts{
				ControlPlaneTopology:         "HighlyAvailable",
				HostedClusterCount:           1,
				HostedControlPlaneNamespaces: []string{"hc01-infra-hc01"},
			},
			want: TopologyManagement,
		},
		{
			name: "hosted external with control plane elsewhere",
			facts: TopologyFacts{
				ControlPlaneTopology: "External",
				ManagedByHypershift:  true,
			},
			want: TopologyHosted,
		},
		{
			name: "external without managed flag still hosted",
			facts: TopologyFacts{
				ControlPlaneTopology: "External",
			},
			want: TopologyHosted,
		},
		{
			name: "standalone highly available is dedicated",
			facts: TopologyFacts{
				ControlPlaneTopology: "HighlyAvailable",
			},
			want: TopologyDedicated,
		},
		{
			name: "single node is dedicated",
			facts: TopologyFacts{
				ControlPlaneTopology: "SingleReplica",
			},
			want: TopologyDedicated,
		},
		{
			name:  "absent facts are unknown never error",
			facts: TopologyFacts{},
			want:  TopologyUnknown,
		},
		{
			name: "unrecognized topology value is unknown",
			facts: TopologyFacts{
				ControlPlaneTopology: "DualReplica",
			},
			want: TopologyUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.facts)
			assert.Equal(t, tc.want, got, "Classify(%+v)", tc.facts)
		})
	}
}

func TestClusterTopologyString(t *testing.T) {
	require.Equal(t, "management", TopologyManagement.String())
	require.Equal(t, "unknown", ClusterTopology("bogus").String())
}

func TestParseClusterTopology(t *testing.T) {
	cases := []struct {
		in   string
		want ClusterTopology
	}{
		{"dedicated", TopologyDedicated},
		{"hosted", TopologyHosted},
		{"management", TopologyManagement},
		{"unknown", TopologyUnknown},
		{"", TopologyUnknown},
		{"bogus", TopologyUnknown},
		{"Hosted", TopologyUnknown},
	}
	for _, tc := range cases {
		t.Run("parse_"+tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseClusterTopology(tc.in))
		})
	}
}
