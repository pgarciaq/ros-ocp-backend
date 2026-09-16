// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func hostedScopeDigestRows() []DigestRow {
	allocCPU := ptr64(16000)
	allocMem := ptr64(65536)
	return []DigestRow{
		makeDigestRow("worker-0", 1, 500, 1000, 2000, 4000, 8000, 32000, allocCPU, allocMem),
		makeDigestRow("worker-0", 2, 600, 1200, 2500, 4500, 8000, 32000, allocCPU, allocMem),
	}
}

func stripHostedScope(recs []Rec) []Rec {
	out := make([]Rec, len(recs))
	for i, r := range recs {
		codes := make([]int16, 0, len(r.NotificationCodes))
		for _, c := range r.NotificationCodes {
			if c != types.NotifNodeHostedScope {
				codes = append(codes, c)
			}
		}
		r.NotificationCodes = codes
		out[i] = r
	}
	return out
}

func TestRecommendNodes_HostedTopologyAppendsScopeCode(t *testing.T) {
	cfg := defaultRecConfig()
	terms := []types.TermConfig{{Name: "short", WindowDays: 7, MinDataDays: 1}}

	hosted := RecommendNodes(hostedScopeDigestRows(), cfg, defaultThresholdSettings, terms, topology.TopologyHosted)
	require.NotEmpty(t, hosted)
	for _, r := range hosted {
		assert.Contains(t, r.NotificationCodes, types.NotifNodeHostedScope,
			"every node rec on hosted topology must carry the scope marker")
	}
}

func TestRecommendNodes_NonHostedTopologyOmitsScopeCode(t *testing.T) {
	cfg := defaultRecConfig()
	terms := []types.TermConfig{{Name: "short", WindowDays: 7, MinDataDays: 1}}

	for _, topo := range []topology.ClusterTopology{
		topology.TopologyUnknown, topology.TopologyDedicated, topology.TopologyManagement,
	} {
		recs := RecommendNodes(hostedScopeDigestRows(), cfg, defaultThresholdSettings, terms, topo)
		for _, r := range recs {
			assert.NotContains(t, r.NotificationCodes, types.NotifNodeHostedScope,
				"topology %v must not emit the hosted marker", topo)
		}
	}
}

func TestRecommendNodes_HostedMarkerIsAdditiveOnly(t *testing.T) {
	cfg := defaultRecConfig()
	terms := []types.TermConfig{{Name: "short", WindowDays: 7, MinDataDays: 1}}

	plain := RecommendNodes(hostedScopeDigestRows(), cfg, defaultThresholdSettings, terms, topology.TopologyUnknown)
	hosted := RecommendNodes(hostedScopeDigestRows(), cfg, defaultThresholdSettings, terms, topology.TopologyHosted)
	require.Len(t, hosted, len(plain), "marker must not change rec count")
	assert.Equal(t, plain, stripHostedScope(hosted),
		"hosted topology must change nothing except the appended code")
}
