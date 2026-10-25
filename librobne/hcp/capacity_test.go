// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// healthyFleet is the hand-verified table fixture for the published-sizing
// math (ADR-0334 baselines). Values were chosen off exact-ratio boundaries
// so float division cannot flip a floor (adversarial-fixtures rule).
//
//	Workers: 64 vCPU allocatable, 128 GiB allocatable, 1500 maxPods
//	Request limits: CPU 64000/5000=12.8, mem 128GiB/18GiB≈7.11, pods 1500/75=20
//	Low(50)  usage: CPU 64/3.35≈19.10, mem 128/11.225≈11.40
//	Avg(300) usage: CPU 64/5.6≈11.43,  mem 128/11.85≈10.80
//	Med(1k)  usage: CPU 64/11.9≈5.38,  mem 128/13.6≈9.41
//	High(2k) usage: CPU 64/20.9≈3.06,  mem 128/16.1≈7.95
func healthyFleet() CapacityInputs {
	return CapacityInputs{
		Workers: WorkerCapacity{
			CPUAllocatableMillicores: 64000,
			MemoryAllocatableBytes:   128 << 30,
			MaxPods:                  1500,
			PlacementKnown:           true,
		},
		CurrentHCCount:          4,
		InventoryComplete:       true,
		ReadyHCPCount:           2,
		AverageReadyHCPQPS:      300,
		ReadyHCPQPSKnown:        true,
		HostedAvailabilityBasis: BasisHighlyAvailable,
	}
}

func scenarioByName(t *testing.T, res CapacityResult, name string) CapacityScenario {
	t.Helper()
	for _, s := range res.Scenarios {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("scenario %q missing from result", name)
	return CapacityScenario{}
}

// The request scenario is floor(min(cpu-request, memory-request, pods))
// with the limiting resource named. Floor matters: 12.8 CPU and 7.11 memory
// must yield 7, not 8 or 7.11 — and the memory leg governs, so
// LimitingResource must name memory_request, not the larger CPU leg.
func TestCalculateCapacity_RequestScenarioFormula(t *testing.T) {
	res, err := CalculateCapacity(healthyFleet())
	require.NoError(t, err)

	assert.Equal(t, ModelVersion, res.ModelVersion, "result must carry the model version")
	require.Len(t, res.Scenarios, 5, "exactly five scenarios")
	names := make([]string, 5)
	for i, s := range res.Scenarios {
		names[i] = s.Name
	}
	assert.Equal(t, []string{ScenarioRequest, ScenarioLowQPS, ScenarioAverageQPS, ScenarioMediumQPS, ScenarioHighQPS}, names,
		"scenario order must be stable for consumers and #700")

	req := scenarioByName(t, res, ScenarioRequest)
	assert.Equal(t, int64(7), req.MaxHCs, "floor(min(12.8, 7.11, 20)) must be 7")
	assert.Equal(t, LimitingMemoryRequest, req.LimitingResource, "memory request leg (7.11) governs over CPU (12.8) and pods (20)")
	require.NotNil(t, req.Headroom, "complete inventory must yield headroom")
	assert.Equal(t, int64(3), *req.Headroom, "7 max - 4 current")
	assert.Equal(t, StatusOK, req.Status, "highly_available fleet has no illustrative warning")
}

// QPS scenarios take the minimum of the request/pod limits AND the usage
// limits (idle + increments per 1000 QPS). Medium's CPU-usage leg (≈5.38)
// and high's (≈3.06) must govern over the request-memory leg (7.11) — a
// QPS scenario that ignored usage limits would report 7. High is over
// capacity: headroom must stay signed negative, never clamped to zero.
func TestCalculateCapacity_QPSUsageLimits(t *testing.T) {
	res, err := CalculateCapacity(healthyFleet())
	require.NoError(t, err)

	med := scenarioByName(t, res, ScenarioMediumQPS)
	assert.Equal(t, int64(5), med.MaxHCs, "floor(min(…, 64/11.9≈5.38, …)) must be 5")
	assert.Equal(t, LimitingCPUUsage, med.LimitingResource, "medium CPU usage leg governs")
	require.NotNil(t, med.Headroom)
	assert.Equal(t, int64(1), *med.Headroom, "5 - 4")

	high := scenarioByName(t, res, ScenarioHighQPS)
	assert.Equal(t, int64(3), high.MaxHCs, "floor(min(…, 64/20.9≈3.06, …)) must be 3")
	assert.Equal(t, LimitingCPUUsage, high.LimitingResource, "high CPU usage leg governs")
	require.NotNil(t, high.Headroom, "over-capacity headroom must still be present")
	assert.Equal(t, int64(-1), *high.Headroom, "3 - 4 must stay signed negative, not clamp to 0")
}

// The average scenario uses the provided observed mean (300 QPS → usage
// legs CPU 64/5.6≈11.43, mem 128/11.85≈10.80; request-memory 7.11 still
// governs). A result equal to the medium (5) or low (7 via a different leg)
// mix-up would show the fallbacks firing instead of the observed value.
func TestCalculateCapacity_AverageScenarioUsesObservedMean(t *testing.T) {
	res, err := CalculateCapacity(healthyFleet())
	require.NoError(t, err)

	avg := scenarioByName(t, res, ScenarioAverageQPS)
	assert.Equal(t, int64(7), avg.MaxHCs, "request-memory leg (7.11) governs at 300 QPS")
	assert.Equal(t, LimitingMemoryRequest, avg.LimitingResource)
	require.NotNil(t, avg.Headroom)
	assert.Equal(t, int64(3), *avg.Headroom)
}

// No Ready HCPs + QPS known → the average scenario falls back to the
// low-QPS (50) basis. A zero-QPS misread would divide by ~2.9 vCPU and
// report a fantasy maximum; the fallback must match the low scenario
// exactly, and a supplied AverageReadyHCPQPS must be ignored.
func TestCalculateCapacity_AverageFallbackToLowBasis(t *testing.T) {
	in := healthyFleet()
	in.ReadyHCPCount = 0
	in.AverageReadyHCPQPS = 0 // must be ignored: no ready HCPs
	res, err := CalculateCapacity(in)
	require.NoError(t, err)

	avg := scenarioByName(t, res, ScenarioAverageQPS)
	low := scenarioByName(t, res, ScenarioLowQPS)
	assert.Equal(t, low.MaxHCs, avg.MaxHCs, "no ready HCPs → 50-QPS low basis, not zero-QPS")
	assert.Equal(t, low.LimitingResource, avg.LimitingResource)
	require.NotNil(t, avg.Headroom)
}

// QPS unknown (query failed or series absent) is NOT the no-ready-HCPs
// case: only the average scenario degrades — MaxHCs 0, nil headroom, no
// limiting resource — while the other four scenarios keep full estimates.
// A NaN value alongside Known=false must not error (the value is unused).
func TestCalculateCapacity_AverageUnknown(t *testing.T) {
	in := healthyFleet()
	in.ReadyHCPQPSKnown = false
	in.AverageReadyHCPQPS = math.NaN()
	res, err := CalculateCapacity(in)
	require.NoError(t, err)

	avg := scenarioByName(t, res, ScenarioAverageQPS)
	assert.Equal(t, int64(0), avg.MaxHCs, "unknown QPS → no average estimate, never the 50 basis")
	assert.Nil(t, avg.Headroom, "unknown QPS → nil headroom")
	assert.Empty(t, avg.LimitingResource, "no estimate → no limiting resource")

	for _, name := range []string{ScenarioRequest, ScenarioLowQPS, ScenarioMediumQPS, ScenarioHighQPS} {
		s := scenarioByName(t, res, name)
		assert.NotNil(t, s.Headroom, "%s must keep its headroom when only QPS is unknown", name)
		assert.Greater(t, s.MaxHCs, int64(0), "%s must keep its estimate", name)
	}
}

// Incomplete HC inventory suppresses headroom on every scenario (unknown
// occupancy, never zero) while the sizing estimates themselves survive —
// the estimate does not depend on the current count.
func TestCalculateCapacity_InventoryIncomplete(t *testing.T) {
	in := healthyFleet()
	in.InventoryComplete = false
	res, err := CalculateCapacity(in)
	require.NoError(t, err)

	for _, s := range res.Scenarios {
		assert.Nil(t, s.Headroom, "%s headroom must be nil on incomplete inventory, never zero-filled", s.Name)
	}
	req := scenarioByName(t, res, ScenarioRequest)
	assert.Equal(t, int64(7), req.MaxHCs, "estimate survives unknown occupancy")
}

// Unknown worker placement fails closed: no estimate at all on any
// scenario (MaxHCs 0, nil headroom, no limiting resource) — not a
// zero-worker "no room" answer, which would read as over capacity.
func TestCalculateCapacity_PlacementUnknown(t *testing.T) {
	in := healthyFleet()
	in.Workers.PlacementKnown = false
	res, err := CalculateCapacity(in)
	require.NoError(t, err)

	for _, s := range res.Scenarios {
		assert.Equal(t, int64(0), s.MaxHCs, "%s must have no estimate on unknown placement", s.Name)
		assert.Nil(t, s.Headroom, "%s headroom must be nil on unknown placement", s.Name)
		assert.Empty(t, s.LimitingResource, "%s must name no limiting resource", s.Name)
	}
}

// The illustrative-only warning keys on the hosted control planes'
// availability aggregate (ADR-0334 rule 2): mixed and single-replica mark
// every scenario illustrative; highly_available, unknown, and empty do
// not. The math must be unchanged by the warning — same MaxHCs and
// headroom as the unmarked run.
func TestCalculateCapacity_AvailabilityBasisWarnings(t *testing.T) {
	cases := []struct {
		basis string
		want  string
		why   string
	}{
		{BasisMixed, StatusIllustrativeOnly, "mixed fleets warn illustrative-only"},
		{BasisSingleReplica, StatusIllustrativeOnly, "single-replica fleets warn illustrative-only"},
		{BasisHighlyAvailable, StatusOK, "HA fleets carry no warning"},
		{BasisUnknown, StatusOK, "unknown basis must not claim non-HA"},
		{"", StatusOK, "empty basis reads as unknown"},
	}
	for _, tc := range cases {
		in := healthyFleet()
		in.HostedAvailabilityBasis = tc.basis
		res, err := CalculateCapacity(in)
		require.NoError(t, err, tc.why)
		for _, s := range res.Scenarios {
			assert.Equal(t, tc.want, s.Status, "%s: %s", s.Name, tc.why)
		}
		req := scenarioByName(t, res, ScenarioRequest)
		assert.Equal(t, int64(7), req.MaxHCs, "%s: warning must not change the math", tc.why)
		require.NotNil(t, req.Headroom, tc.why)
		assert.Equal(t, int64(3), *req.Headroom, "%s: warning must not change headroom", tc.why)
	}
}

// Management controlPlaneTopology and raw ControllerAvailabilityPolicy
// spellings are not basis values: the collector must aggregate to the
// snake_case enum, and a raw value sneaking in must error rather than
// silently reading as highly_available (the management cluster's own
// topology must never drive the hosted-availability warning).
func TestCalculateCapacity_ControlPlaneTopologyIsNotABasis(t *testing.T) {
	for _, basis := range []string{"HighlyAvailable", "SingleReplica", "External", "highly-available"} {
		in := healthyFleet()
		in.HostedAvailabilityBasis = basis
		_, err := CalculateCapacity(in)
		assert.Error(t, err, "basis %q must be rejected — only the aggregated enum is valid", basis)
	}
}

// Garbage in must error, never produce a number: negative capacities,
// negative counts, and non-finite or negative QPS when Known.
func TestCalculateCapacity_InvalidInputs(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*CapacityInputs)
		why  string
	}{
		{"negative cpu", func(in *CapacityInputs) { in.Workers.CPUAllocatableMillicores = -1 }, "negative capacity is measurement error"},
		{"negative memory", func(in *CapacityInputs) { in.Workers.MemoryAllocatableBytes = -1 }, "negative capacity is measurement error"},
		{"negative pods", func(in *CapacityInputs) { in.Workers.MaxPods = -1 }, "negative capacity is measurement error"},
		{"negative hc count", func(in *CapacityInputs) { in.CurrentHCCount = -1 }, "negative occupancy is measurement error"},
		{"negative ready count", func(in *CapacityInputs) { in.ReadyHCPCount = -1 }, "negative occupancy is measurement error"},
		{"nan qps", func(in *CapacityInputs) { in.AverageReadyHCPQPS = math.NaN() }, "non-finite QPS must error when the value is used"},
		{"inf qps", func(in *CapacityInputs) { in.AverageReadyHCPQPS = math.Inf(1) }, "non-finite QPS must error when the value is used"},
		{"negative qps", func(in *CapacityInputs) { in.AverageReadyHCPQPS = -1 }, "negative QPS is measurement error"},
	}
	for _, tc := range cases {
		in := healthyFleet()
		tc.mut(&in)
		_, err := CalculateCapacity(in)
		assert.Error(t, err, "%s: %s", tc.name, tc.why)
	}
}

// Zero eligible workers is a truthful "no room" — MaxHCs 0 with signed
// negative headroom — not an error and not a nil dodge. A nil here would
// hide a real over-capacity state on a drained fleet.
func TestCalculateCapacity_ZeroWorkers(t *testing.T) {
	in := healthyFleet()
	in.Workers = WorkerCapacity{PlacementKnown: true}
	res, err := CalculateCapacity(in)
	require.NoError(t, err)

	for _, s := range res.Scenarios {
		assert.Equal(t, int64(0), s.MaxHCs, "%s: zero workers → zero max", s.Name)
		require.NotNil(t, s.Headroom, "%s: zero workers must yield signed headroom, not nil", s.Name)
		assert.Equal(t, int64(-4), *s.Headroom, "%s: 0 - 4 over capacity", s.Name)
	}
}
