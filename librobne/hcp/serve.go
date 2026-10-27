// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	"strconv"
	"strings"
	"time"
)

// Serve-time derivations for the W4 fleet-capacity advisory (#392, #700).
// Shared by the API handler (internal/api) and the robne CLI: the rules
// live here, once; each consumer keeps its own JSON DTO and maps into the
// ServeObservation view. The API side is guarded by characterization
// goldens captured before this extraction — behavior must stay identical.
//
// # Shape map (round-9 N1)
//
// Three row shapes cross this seam; the suffix is the layer contract:
//
//   - `…Row` (csv.HCPCapacityRow) — the wire/CSV trust boundary: raw
//     inputs, no cluster identity (Kafka/manifest-supplied).
//   - `…Observation` (ServeObservation, this file) — the DB-free derived
//     view the rules consume: inputs plus computed scenarios.
//   - `…Out`/response (per consumer) — presentation DTOs.
//
// Mappers are trivial field copies owned by each consumer.

// ServeObservation is one capacity observation with its computed
// scenarios, in the shape the derivation rules consume. Placement lives
// in Workers.PlacementKnown (no duplicate top-level field — round-9 W7).
type ServeObservation struct {
	ObservedAt           time.Time
	MCESampledAt         *time.Time
	OperatorVersion      string
	OCPVersion           string
	MCEVersion           string
	NativeModelVersion   string
	CollectionMinutes    int64
	UploadMinutes        int64
	InventoryComplete    bool
	QPSKnown             bool
	QPS                  *float64
	CurrentHC            int64
	ReadyHC              int64
	Basis                string
	Pressure             string
	SharedInfrastructure bool
	Workers              WorkerCapacity
	MCERequest           *int64
	MCELow               *int64
	MCEAverage           *int64
	MCEMedium            *int64
	MCEHigh              *int64
	Scenarios            []CapacityScenario
}

// ServeWarnings is the derived caution set. MCEMayBeStale is a
// pass-through: the consumer computes it via MCEMayBeStaleFromSeries (the
// API from its DB window query, the CLI from the in-payload series) and
// supplies it here (round-9 W1).
type ServeWarnings struct {
	Stale                bool
	BelowVersionFloor    bool
	Pressured            bool
	PressureUnknown      bool
	SharedInfrastructure bool
	MCEMayBeStale        *bool
}

// The documented OCP management version floor (ADR-0334 amendment: OCP
// 4.17+ / MCE 2.6+ within the official pair matrix).
const (
	serveOCPFloorMajor = 4
	serveOCPFloorMinor = 17
)

// FreshnessWindow is 2× the row's own effective cadence: a 6-hour upload
// cycle must not mark a fresh hourly row stale.
func FreshnessWindow(collectionMinutes, uploadMinutes int64) time.Duration {
	cadence := collectionMinutes
	if uploadMinutes > cadence {
		cadence = uploadMinutes
	}
	return 2 * time.Duration(cadence) * time.Minute
}

// BelowVersionFloor reports a known ocp_version below the documented
// floor. Unparseable or unknown versions never warn (only a clearly
// below-floor version does).
func BelowVersionFloor(ocpVersion string) bool {
	if ocpVersion == "" {
		return false
	}
	parts := strings.SplitN(ocpVersion, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major < serveOCPFloorMajor ||
		(major == serveOCPFloorMajor && minor < serveOCPFloorMinor)
}

// ScenarioStatus maps the availability basis to the scenario status:
// mixed or single-replica hosted control planes make the estimates
// illustrative-only (ADR-0334 rule 2 — math unchanged).
func ScenarioStatus(basis string) string {
	if basis == BasisMixed || basis == BasisSingleReplica {
		return StatusIllustrativeOnly
	}
	return StatusOK
}

// DeriveHeadroom applies the serve-time nil rules (round-9 B4: the
// scenario name is required — the average+unknown-QPS rule cannot be
// expressed without it). Headroom is nil on: no estimate (empty limiting
// resource), incomplete inventory, unknown worker placement, affirmative
// pressure (withheld on every scenario, ADR-0334 rule 8), or the average
// scenario with unknown QPS. Otherwise it is the signed max − current.
func DeriveHeadroom(o ServeObservation, scenario string, maxHCs int64, limiting string) *int64 {
	if limiting == "" || !o.InventoryComplete || !o.Workers.PlacementKnown || o.Pressure == "pressured" {
		return nil
	}
	if scenario == ScenarioAverageQPS && !o.QPSKnown {
		return nil
	}
	h := maxHCs - o.CurrentHC
	return &h
}

// DeriveWarnings computes the caution set at `now`. Unknown pressure
// stays an explicit warning; affirmative pressure flags Pressured.
func DeriveWarnings(o ServeObservation, now time.Time, mceMayBeStale *bool) ServeWarnings {
	age := now.Sub(o.ObservedAt)
	if age < 0 {
		age = 0
	}
	return ServeWarnings{
		Stale:                age > FreshnessWindow(o.CollectionMinutes, o.UploadMinutes),
		BelowVersionFloor:    BelowVersionFloor(o.OCPVersion),
		Pressured:            o.Pressure == "pressured",
		PressureUnknown:      o.Pressure == "unknown",
		SharedInfrastructure: o.SharedInfrastructure,
		MCEMayBeStale:        mceMayBeStale,
	}
}

// MCEMayBeStaleFromSeries implements the frozen-reference caution over an
// assembled series: newest-first, window = FreshnessWindow of series[0]
// (relative to series[0], not an external clock). True iff all in-window
// priors share series[0]'s five MCE references while any native input
// (worker capacity, occupancy, traffic basis) moved. len < 2 → false.
// Supersedes the design's MCEStalePoint sketch (round-9 W9).
func MCEMayBeStaleFromSeries(series []ServeObservation) bool {
	if len(series) < 2 {
		return false
	}
	latest := series[0]
	window := FreshnessWindow(latest.CollectionMinutes, latest.UploadMinutes)
	mceSame := true
	nativeChanged := false
	for _, prior := range series[1:] {
		if prior.ObservedAt.Before(latest.ObservedAt.Add(-window)) {
			continue
		}
		mceSame = mceSame && serveMCEEqual(prior, latest)
		nativeChanged = nativeChanged || serveNativeDiffers(prior, latest)
	}
	return mceSame && nativeChanged
}

// serveMCEEqual compares the five MCE reference gauges (NULL is a value:
// absent == absent, absent != zero).
func serveMCEEqual(a, b ServeObservation) bool {
	eq := func(x, y *int64) bool {
		if x == nil || y == nil {
			return x == y
		}
		return *x == *y
	}
	return eq(a.MCERequest, b.MCERequest) &&
		eq(a.MCELow, b.MCELow) &&
		eq(a.MCEAverage, b.MCEAverage) &&
		eq(a.MCEMedium, b.MCEMedium) &&
		eq(a.MCEHigh, b.MCEHigh)
}

// serveNativeDiffers reports whether the native inputs moved between two
// observations (worker capacity, occupancy, or traffic basis).
func serveNativeDiffers(a, b ServeObservation) bool {
	qpsDiffers := func() bool {
		if a.QPS == nil || b.QPS == nil {
			return a.QPS != b.QPS
		}
		return *a.QPS != *b.QPS
	}
	return a.Workers.CPUAllocatableMillicores != b.Workers.CPUAllocatableMillicores ||
		a.Workers.MemoryAllocatableBytes != b.Workers.MemoryAllocatableBytes ||
		a.Workers.MaxPods != b.Workers.MaxPods ||
		a.CurrentHC != b.CurrentHC ||
		a.ReadyHC != b.ReadyHC ||
		a.QPSKnown != b.QPSKnown ||
		qpsDiffers()
}
