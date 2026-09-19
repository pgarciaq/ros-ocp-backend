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
	generic := map[string]ContainerRec{}
	for _, r := range byNS["app-ns"] {
		generic[r.Term+"\x00"+r.Engine] = r
	}
	for _, r := range byNS["hc01-infra-hc01"] {
		g := generic[r.Term+"\x00"+r.Engine]
		assert.GreaterOrEqual(t, r.RecCPURequestMC, g.RecCPURequestMC, "guardrail must not lower CPU vs generic")
		assert.GreaterOrEqual(t, r.RecMemRequestKiB, g.RecMemRequestKiB, "guardrail must not lower memory vs generic")
	}
}
