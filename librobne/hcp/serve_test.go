package hcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Serve-seam tests (#700 Task 2): the derivation rules shared by the API
// handler and the robne CLI. Hand-computed expectations from the
// published-sizing baselines; the API characterization goldens guard the
// handler side byte-for-byte.

func serveFixture() ServeObservation {
	// The Task 2 fixture: 64 vCPU / 128 GiB / 1500 pods, 4 HCs, 2 ready,
	// 300 QPS known, highly_available, complete inventory, known
	// placement, unknown pressure. Stored scenarios: request/low/avg 7
	// memory_request, medium 5 cpu_usage, high 3 cpu_usage.
	qps := 300.0
	cpu, mem, pods := int64(64000), int64(137438953472), int64(1500)
	mceReq, mceAvg, mceMed, mceHigh := int64(8), int64(8), int64(4), int64(2)
	return ServeObservation{
		ObservedAt:         time.Date(2026, 10, 25, 14, 59, 59, 0, time.UTC),
		OperatorVersion:    "deadbeef",
		OCPVersion:         "4.22.17",
		NativeModelVersion: ModelVersion,
		CollectionMinutes:  60,
		UploadMinutes:      60,
		InventoryComplete:  true,
		QPSKnown:           true,
		QPS:                &qps,
		CurrentHC:          4,
		ReadyHC:            2,
		Basis:              BasisHighlyAvailable,
		Pressure:           "unknown",
		Workers:            WorkerCapacity{CPUAllocatableMillicores: cpu, MemoryAllocatableBytes: mem, MaxPods: pods, PlacementKnown: true},
		MCERequest:         &mceReq,
		MCEAverage:         &mceAvg,
		MCEMedium:          &mceMed,
		MCEHigh:            &mceHigh,
		Scenarios: []CapacityScenario{
			{Name: ScenarioRequest, MaxHCs: 7, LimitingResource: "memory_request"},
			{Name: ScenarioLowQPS, MaxHCs: 7, LimitingResource: "memory_request"},
			{Name: ScenarioAverageQPS, MaxHCs: 7, LimitingResource: "memory_request"},
			{Name: ScenarioMediumQPS, MaxHCs: 5, LimitingResource: "cpu_usage"},
			{Name: ScenarioHighQPS, MaxHCs: 3, LimitingResource: "cpu_usage"},
		},
	}
}

func TestFreshnessWindow(t *testing.T) {
	// 2× max cadence (round-9 B3: 60/60 → 2h; the 12h figure belongs to
	// the API fixture's 60/360).
	assert.Equal(t, 2*time.Hour, FreshnessWindow(60, 60))
	assert.Equal(t, 12*time.Hour, FreshnessWindow(60, 360))
	assert.Equal(t, 12*time.Hour, FreshnessWindow(360, 60))
}

func TestBelowVersionFloor(t *testing.T) {
	assert.False(t, BelowVersionFloor(""))
	assert.False(t, BelowVersionFloor("4.17.0"))
	assert.False(t, BelowVersionFloor("4.22.17"))
	assert.True(t, BelowVersionFloor("4.16.9"))
	assert.True(t, BelowVersionFloor("3.11.0"))
	assert.False(t, BelowVersionFloor("not-a-version"))
	assert.False(t, BelowVersionFloor("4"))
}

func TestScenarioStatus(t *testing.T) {
	assert.Equal(t, StatusOK, ScenarioStatus(BasisHighlyAvailable))
	assert.Equal(t, StatusOK, ScenarioStatus(BasisUnknown))
	assert.Equal(t, StatusOK, ScenarioStatus(""))
	assert.Equal(t, StatusIllustrativeOnly, ScenarioStatus(BasisMixed))
	assert.Equal(t, StatusIllustrativeOnly, ScenarioStatus(BasisSingleReplica))
}

func TestDeriveHeadroom(t *testing.T) {
	o := serveFixture()

	// Healthy: signed headroom on every scenario.
	h := DeriveHeadroom(o, ScenarioRequest, 7, "memory_request")
	require.NotNil(t, h)
	assert.Equal(t, int64(3), *h)
	h = DeriveHeadroom(o, ScenarioHighQPS, 3, "cpu_usage")
	require.NotNil(t, h)
	assert.Equal(t, int64(-1), *h, "over-capacity stays signed")

	// No estimate: empty limiting resource.
	assert.Nil(t, DeriveHeadroom(o, ScenarioAverageQPS, 0, ""))

	// Incomplete inventory: withheld.
	incomplete := o
	incomplete.InventoryComplete = false
	assert.Nil(t, DeriveHeadroom(incomplete, ScenarioRequest, 7, "memory_request"))

	// Unknown placement: withheld (placement lives in Workers).
	unknownPlacement := o
	unknownPlacement.Workers.PlacementKnown = false
	assert.Nil(t, DeriveHeadroom(unknownPlacement, ScenarioRequest, 7, "memory_request"))

	// Affirmative pressure: withheld on every scenario (round-9 B4: the
	// scenario parameter expresses the average-scenario rule).
	pressured := o
	pressured.Pressure = "pressured"
	for _, name := range []string{ScenarioRequest, ScenarioLowQPS, ScenarioAverageQPS, ScenarioMediumQPS, ScenarioHighQPS} {
		assert.Nil(t, DeriveHeadroom(pressured, name, 7, "memory_request"), "%s withheld under pressure", name)
	}

	// Unknown average QPS degrades only the average scenario.
	unknownQPS := o
	unknownQPS.QPSKnown = false
	unknownQPS.QPS = nil
	assert.Nil(t, DeriveHeadroom(unknownQPS, ScenarioAverageQPS, 7, "memory_request"))
	assert.NotNil(t, DeriveHeadroom(unknownQPS, ScenarioRequest, 7, "memory_request"))
}

func TestDeriveWarnings(t *testing.T) {
	o := serveFixture()
	now := o.ObservedAt.Add(30 * time.Minute)

	fresh := DeriveWarnings(o, now, nil)
	assert.False(t, fresh.Stale)
	assert.False(t, fresh.BelowVersionFloor)
	assert.False(t, fresh.Pressured)
	assert.True(t, fresh.PressureUnknown)
	assert.Nil(t, fresh.MCEMayBeStale, "caution passes through, not derived here")

	stale := DeriveWarnings(o, o.ObservedAt.Add(3*time.Hour), nil) // window 2h
	assert.True(t, stale.Stale)

	below := o
	below.OCPVersion = "4.16.9"
	assert.True(t, DeriveWarnings(below, now, nil).BelowVersionFloor)

	pressured := o
	pressured.Pressure = "pressured"
	w := DeriveWarnings(pressured, now, nil)
	assert.True(t, w.Pressured)
	assert.False(t, w.PressureUnknown)

	compact := o
	compact.SharedInfrastructure = true
	assert.True(t, DeriveWarnings(compact, now, nil).SharedInfrastructure)

	caution := true
	assert.True(t, DeriveWarnings(o, now, &caution).MCEMayBeStale != nil && *DeriveWarnings(o, now, &caution).MCEMayBeStale)
}

func TestMCEMayBeStaleFromSeries(t *testing.T) {
	// Series is newest-first; the window comes from series[0]'s cadence
	// (60/60 → 2h), NOT from any external clock.
	latest := serveFixture()
	prior := serveFixture()
	movedCPU := serveFixture()
	cpu := int64(65000)
	movedCPU.Workers.CPUAllocatableMillicores = cpu

	t.Run("single observation is false", func(t *testing.T) {
		assert.False(t, MCEMayBeStaleFromSeries([]ServeObservation{latest}))
	})
	t.Run("frozen refs with moving native inputs is true", func(t *testing.T) {
		assert.True(t, MCEMayBeStaleFromSeries([]ServeObservation{latest, movedCPU}))
	})
	t.Run("frozen refs with identical inputs is false", func(t *testing.T) {
		assert.False(t, MCEMayBeStaleFromSeries([]ServeObservation{latest, prior}))
	})
	t.Run("moving refs is false", func(t *testing.T) {
		changedRef := serveFixture()
		v := int64(9)
		changedRef.MCERequest = &v
		assert.False(t, MCEMayBeStaleFromSeries([]ServeObservation{latest, changedRef}))
	})
	t.Run("prior outside the window is ignored", func(t *testing.T) {
		old := serveFixture()
		old.ObservedAt = latest.ObservedAt.Add(-3 * time.Hour) // window is 2h
		assert.False(t, MCEMayBeStaleFromSeries([]ServeObservation{latest, old}))
	})
	t.Run("native movement includes QPS-known flips and counts", func(t *testing.T) {
		changedCounts := serveFixture()
		changedCounts.CurrentHC = 5
		assert.True(t, MCEMayBeStaleFromSeries([]ServeObservation{latest, changedCounts}))
		changedQPSKnown := serveFixture()
		changedQPSKnown.QPSKnown = false
		changedQPSKnown.QPS = nil
		assert.True(t, MCEMayBeStaleFromSeries([]ServeObservation{latest, changedQPSKnown}))
	})
}
