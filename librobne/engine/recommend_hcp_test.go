// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HCP guardrail routing (#584, detect-and-route): identical rows in an HCP
// namespace take the controlplane floor profile, identical rows elsewhere
// take the generic profile, and every row still produces recommendations.
func TestRecommendWorkloads_HCPGuardrailRouting(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	mkRows := func(namespace string) []KeyedDigest {
		// Request 1000m / 512MiB, usage one quarter: relative floor governs
		// (700m > 100m absolute; 367001KiB > 128MiB absolute), generic
		// usage-driven recs land well below both.
		rows := make([]KeyedDigest, 0, 14)
		key := ContainerKey{Namespace: namespace, Workload: "etcd", WorkloadType: "StatefulSet", ContainerName: "etcd"}
		for d := 0; d < 14; d++ {
			rows = append(rows, KeyedDigest{Key: key, Row: syntheticDigestRow(start.AddDate(0, 0, d), 1000, 524288)})
		}
		return rows
	}
	rows := append(mkRows("hc01-infra-hc01"), mkRows("app-ns")...)

	cfg := DefaultEngineConfig("org", "cluster", start.AddDate(0, 0, 14))
	cfg.HCPNamespaces = []string{"hc01-infra-hc01"}

	var recs []ContainerRec
	err := RecommendWorkloads(context.Background(), rows, cfg, func(batch []ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	require.NoError(t, err)
	// Detect-not-exclude: both containers emit the full 3 terms × 2 engines.
	require.Len(t, recs, 12)

	byNS := map[string][]ContainerRec{}
	for _, r := range recs {
		byNS[r.Namespace] = append(byNS[r.Namespace], r)
	}
	require.Len(t, byNS["hc01-infra-hc01"], 6)
	require.Len(t, byNS["app-ns"], 6)

	for _, r := range byNS["hc01-infra-hc01"] {
		// Relative floor governs: 70% of the 1000m / 512MiB current request.
		assert.GreaterOrEqual(t, r.RecCPURequestMC, int64(700), "HCP row must take the guardrail CPU floor")
		assert.GreaterOrEqual(t, r.RecMemRequestKiB, int64(367001), "HCP row must take the guardrail memory floor")
	}
	// Floors only raise: identical HCP rows must recommend at least as much
	// as identical generic rows, term by term and engine by engine.
	generic := map[termEngine]ContainerRec{}
	for _, r := range byNS["app-ns"] {
		generic[termEngine{Term: r.Term, Engine: r.Engine}] = r
	}
	for _, r := range byNS["hc01-infra-hc01"] {
		g := generic[termEngine{Term: r.Term, Engine: r.Engine}]
		assert.GreaterOrEqual(t, r.RecCPURequestMC, g.RecCPURequestMC, "guardrail must not lower CPU vs generic")
		assert.GreaterOrEqual(t, r.RecMemRequestKiB, g.RecMemRequestKiB, "guardrail must not lower memory vs generic")
	}
}

type termEngine struct {
	Term   string
	Engine string
}

// Relative floor uses the window median request, not the latest bucket
// (W1 review W2): a redeploy dropping requests in the newest interval must
// not collapse guardrail protection. 13 buckets at 1000m plus a final
// bucket at 10m. Medium/long windows still see median 1000m (floor 700m);
// the 1-row short window sees only the dropped bucket, where the absolute
// backstop (100m) governs instead of the collapsed relative leg (7m).
// That split is the design: median resists anomalies wherever the window
// has the rows to do so, absolute covers the rest.
func TestRecommendWorkloads_HCPFloorUsesMedianRequest(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]KeyedDigest, 0, 14)
	key := ContainerKey{Namespace: "hc01-infra-hc01", Workload: "etcd", WorkloadType: "StatefulSet", ContainerName: "etcd"}
	for d := 0; d < 14; d++ {
		row := syntheticDigestRow(start.AddDate(0, 0, d), 1000, 524288)
		if d == 13 {
			row.CPURequestP50MC = 10
			row.CPURequestP60MC = 10
			row.CPURequestP95MC = 10
			row.MemRequestP50KiB = 102400
			row.MemRequestP60KiB = 102400
			row.MemRequestP95KiB = 102400
		}
		rows = append(rows, KeyedDigest{Key: key, Row: row})
	}

	cfg := DefaultEngineConfig("org", "cluster", start.AddDate(0, 0, 14))
	cfg.HCPNamespaces = []string{"hc01-infra-hc01"}

	var recs []ContainerRec
	err := RecommendWorkloads(context.Background(), rows, cfg, func(batch []ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, recs, 6)
	for _, r := range recs {
		if r.Term == "short" {
			assert.GreaterOrEqual(t, r.RecCPURequestMC, int64(100), "short window sees only the dropped bucket: absolute backstop must govern")
			assert.GreaterOrEqual(t, r.RecMemRequestKiB, int64(131072), "short window sees only the dropped bucket: absolute backstop must govern")
			continue
		}
		assert.GreaterOrEqual(t, r.RecCPURequestMC, int64(700), "dropped latest bucket must not collapse the CPU floor")
		assert.GreaterOrEqual(t, r.RecMemRequestKiB, int64(367001), "dropped latest bucket must not collapse the memory floor")
	}
}

// HCP groups never receive replica recommendations (#584 W1 review finding:
// statefulsetMinReplicas=1 would let idle-etcd recs destroy quorum).
// Operators own control-plane topology; the engine must not opine on it.
// Same fixture shape as the routing test but 3 replicas: pre-fix the HCP
// recs carry a computed (reduced) replica count.
func TestRecommendWorkloads_HCPReplicaRecsSuppressed(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	mkRows := func(namespace string) []KeyedDigest {
		rows := make([]KeyedDigest, 0, 14)
		key := ContainerKey{Namespace: namespace, Workload: "etcd", WorkloadType: "StatefulSet", ContainerName: "etcd"}
		for d := 0; d < 14; d++ {
			row := syntheticDigestRow(start.AddDate(0, 0, d), 1000, 524288)
			row.DesiredReplicas = 3
			row.AvailableReplicas = 3
			rows = append(rows, KeyedDigest{Key: key, Row: row})
		}
		return rows
	}
	rows := append(mkRows("hc01-infra-hc01"), mkRows("app-ns")...)

	cfg := DefaultEngineConfig("org", "cluster", start.AddDate(0, 0, 14))
	cfg.HCPNamespaces = []string{"hc01-infra-hc01"}

	var recs []ContainerRec
	err := RecommendWorkloads(context.Background(), rows, cfg, func(batch []ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, recs, 12)

	byNS := map[string][]ContainerRec{}
	for _, r := range recs {
		byNS[r.Namespace] = append(byNS[r.Namespace], r)
	}
	// Generic path still computes replicas (path runs; proves suppression
	// is HCP-scoped, not a global break).
	for _, r := range byNS["app-ns"] {
		assert.Greater(t, r.RecommendedReplicas, int64(0), "generic groups must still compute replica recs")
	}
	// HCP path suppresses: unset reads as zero.
	for _, r := range byNS["hc01-infra-hc01"] {
		assert.Zero(t, r.RecommendedReplicas, "HCP groups must not receive replica recs")
	}
}
