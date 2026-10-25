// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package hcp

import (
	"fmt"
	"math"
)

// ModelVersion identifies the native published-sizing calculation and its
// baselines; it travels in every snapshot and history row so consumers can
// distinguish rule changes over time (ADR-0334 amendment, #392).
const ModelVersion = "hcp-published-sizing-v1"

// Scenario names in the stable order CalculateCapacity returns them.
// Consumers (ingest columns, API responses, #700 CLI siblings) index by
// name; the order is part of the contract.
const (
	ScenarioRequest    = "request"
	ScenarioLowQPS     = "low_qps"
	ScenarioAverageQPS = "average_qps"
	ScenarioMediumQPS  = "medium_qps"
	ScenarioHighQPS    = "high_qps"
)

// Published sizing baselines from the OpenShift HCP sizing guide (HA
// scenario; ADR-0334). These are planning numbers with named assumptions,
// not measured physics: cloud/arch/version/API load change the answer.
const (
	cpuRequestPerHCMillicores   int64   = 5000     // 5 vCPU requests per HA HC
	memRequestPerHCBytes        int64   = 18 << 30 // 18 GiB requests per HA HC
	podsPerHC                   int64   = 75       // ~75 pods per HA HC for maxPods planning
	idleCPUUsagePerHCMillicores float64 = 2900     // 2.9 vCPU idle usage per HC
	idleMemUsagePerHCGiB        float64 = 11.1     // 11.1 GiB idle usage per HC
	cpuPer1kQPSMillicores       float64 = 9000     // +9 vCPU per +1000 QPS API stress
	memPer1kQPSGiB              float64 = 2.5      // +2.5 GiB per +1000 QPS
	lowQPSBasis                 float64 = 50
	mediumQPSBasis              float64 = 1000
	highQPSBasis                float64 = 2000
)

// bytesPerGiB keeps the usage-leg memory math in binary GiB, matching the
// guide's units.
const bytesPerGiB = float64(1 << 30)

// Limiting-resource labels explain which leg of the minimum produced a
// scenario's maximum.
const (
	LimitingCPURequest    = "cpu_request"
	LimitingMemoryRequest = "memory_request"
	LimitingPods          = "pods"
	LimitingCPUUsage      = "cpu_usage"
	LimitingMemoryUsage   = "memory_usage"
)

// HostedAvailabilityBasis values: the aggregate of the hosted control
// planes' ControllerAvailabilityPolicy, computed by the collector. Never
// the management cluster's controlPlaneTopology (ADR-0334 rule 2: the
// sizing guide assumes HA hosted control planes; mixed or single-replica
// fleets get an illustrative-only warning, not different math).
const (
	BasisHighlyAvailable = "highly_available"
	BasisMixed           = "mixed"
	BasisSingleReplica   = "single_replica"
	BasisUnknown         = "unknown"
)

// Scenario status values. Headroom nil-ness (not Status) signals that a
// scenario has no answer: unknown occupancy, unknown placement, or unknown
// QPS — the cause lives in the snapshot's row-level flags.
const (
	StatusOK               = "ok"
	StatusIllustrativeOnly = "illustrative_only"
)

// WorkerCapacity is the aggregate capacity of the worker set eligible to
// host HCP control planes (default placement: every worker-role node;
// PlacementKnown is false only for genuinely ambiguous constraint sets).
type WorkerCapacity struct {
	CPUAllocatableMillicores int64
	MemoryAllocatableBytes   int64
	MaxPods                  int64
	PlacementKnown           bool
}

// CapacityInputs is the pure sizing input for one management-cluster
// observation. CurrentHCCount is the conservative occupancy (every visible
// HostedCluster object); ReadyHCPCount/AverageReadyHCPQPS carry the
// Ready-HCP traffic basis MCE's average gauge uses — the native average is
// the plain mean Σ/N and must not replicate MCE's 50-seeded-total quirk.
// Pressure is deliberately absent: it is a serving-side gate, not sizing
// math (ADR-0334 amendment).
type CapacityInputs struct {
	Workers                 WorkerCapacity
	CurrentHCCount          int64
	InventoryComplete       bool
	ReadyHCPCount           int64
	AverageReadyHCPQPS      float64
	ReadyHCPQPSKnown        bool
	HostedAvailabilityBasis string
}

// CapacityScenario is one named estimate. MaxHCs is the estimated maximum
// hosted clusters under the scenario's assumptions; Headroom is
// MaxHCs - CurrentHCCount, nil when occupancy or the estimate itself is
// unknown (never zero-filled); LimitingResource names the minimum's
// winning leg ("" when there is no estimate).
type CapacityScenario struct {
	Name             string
	MaxHCs           int64
	Headroom         *int64
	LimitingResource string
	Status           string
}

// CapacityResult is the five-scenario answer plus the model identity.
type CapacityResult struct {
	ModelVersion string
	Scenarios    []CapacityScenario
}

// CalculateCapacity computes the five published-sizing scenarios (request,
// low/average/medium/high QPS) for one observation. The request scenario
// is floor(min(cpu-request, memory-request, pods) packing); QPS scenarios
// additionally take the minimum with the usage legs (idle + increments per
// 1000 QPS). Unknown placement yields no estimate at all; incomplete
// inventory yields estimates with nil headroom; unknown QPS degrades only
// the average scenario. Invalid inputs (negative values, non-finite QPS,
// unmapped availability basis) return an error — garbage never produces a
// number.
func CalculateCapacity(in CapacityInputs) (CapacityResult, error) {
	if err := in.validate(); err != nil {
		return CapacityResult{}, err
	}

	status := StatusOK
	if in.HostedAvailabilityBasis == BasisMixed || in.HostedAvailabilityBasis == BasisSingleReplica {
		status = StatusIllustrativeOnly
	}

	res := CapacityResult{ModelVersion: ModelVersion, Scenarios: make([]CapacityScenario, 0, 5)}

	// Unknown placement fails closed: no estimate on any scenario. A
	// zero-worker fleet is different — it gets a truthful MaxHCs 0 with
	// signed negative headroom below.
	if !in.Workers.PlacementKnown {
		for _, name := range []string{ScenarioRequest, ScenarioLowQPS, ScenarioAverageQPS, ScenarioMediumQPS, ScenarioHighQPS} {
			res.Scenarios = append(res.Scenarios, CapacityScenario{Name: name, Status: status})
		}
		return res, nil
	}

	appendScenario := func(name string, maxHCs int64, limit string) {
		s := CapacityScenario{Name: name, MaxHCs: maxHCs, LimitingResource: limit, Status: status}
		if in.InventoryComplete {
			h := maxHCs - in.CurrentHCCount
			s.Headroom = &h
		}
		res.Scenarios = append(res.Scenarios, s)
	}

	req := requestLimits(in.Workers)
	appendScenario(ScenarioRequest, req.maxHCs, req.limit)

	low := qpsLimits(in.Workers, req, lowQPSBasis)
	appendScenario(ScenarioLowQPS, low.maxHCs, low.limit)

	// Average scenario: unknown QPS is not the no-ready-HCPs case — a
	// failed query degrades only this scenario, never the 50 basis.
	if !in.ReadyHCPQPSKnown {
		res.Scenarios = append(res.Scenarios, CapacityScenario{Name: ScenarioAverageQPS, Status: status})
	} else {
		qps := in.AverageReadyHCPQPS
		if in.ReadyHCPCount == 0 {
			qps = lowQPSBasis
		}
		avg := qpsLimits(in.Workers, req, qps)
		appendScenario(ScenarioAverageQPS, avg.maxHCs, avg.limit)
	}

	medium := qpsLimits(in.Workers, req, mediumQPSBasis)
	appendScenario(ScenarioMediumQPS, medium.maxHCs, medium.limit)

	high := qpsLimits(in.Workers, req, highQPSBasis)
	appendScenario(ScenarioHighQPS, high.maxHCs, high.limit)

	return res, nil
}

// capacityLimit pairs a maximum with the leg that produced it.
type capacityLimit struct {
	maxHCs int64
	limit  string
}

// requestLimits computes the request-packing legs with integer division
// (inputs are integers; floor(min(a,b,c)) == min(floor(a),floor(b),floor(c))
// because floor is monotone, so per-leg flooring is exact).
func requestLimits(w WorkerCapacity) capacityLimit {
	return minLimit([]capacityLimit{
		{w.CPUAllocatableMillicores / cpuRequestPerHCMillicores, LimitingCPURequest},
		{w.MemoryAllocatableBytes / memRequestPerHCBytes, LimitingMemoryRequest},
		{w.MaxPods / podsPerHC, LimitingPods},
	})
}

// qpsLimits takes the minimum of the request/pod legs and the usage legs
// (idle usage + increments per 1000 QPS, matching the sizing guide's load
// table). Usage legs divide integer capacities by fractional per-HC costs,
// so they floor after the division.
func qpsLimits(w WorkerCapacity, req capacityLimit, qps float64) capacityLimit {
	cpuPerHC := idleCPUUsagePerHCMillicores + cpuPer1kQPSMillicores*(qps/1000)
	memPerHCBytes := (idleMemUsagePerHCGiB + memPer1kQPSGiB*(qps/1000)) * bytesPerGiB
	return minLimit([]capacityLimit{
		req,
		{int64(math.Floor(float64(w.CPUAllocatableMillicores) / cpuPerHC)), LimitingCPUUsage},
		{int64(math.Floor(float64(w.MemoryAllocatableBytes) / memPerHCBytes)), LimitingMemoryUsage},
	})
}

// minLimit returns the smallest limit; ties resolve to the earliest leg in
// slice order, keeping the limiting-resource label deterministic.
func minLimit(limits []capacityLimit) capacityLimit {
	best := limits[0]
	for _, l := range limits[1:] {
		if l.maxHCs < best.maxHCs {
			best = l
		}
	}
	return best
}

// validate rejects measurement errors and unmapped enum values. A raw
// controlPlaneTopology or CamelCase ControllerAvailabilityPolicy spelling
// fails here on purpose: the collector must aggregate to the basis enum,
// and the management cluster's own topology must never drive the
// hosted-availability warning.
func (in CapacityInputs) validate() error {
	if in.Workers.CPUAllocatableMillicores < 0 {
		return fmt.Errorf("hcp capacity: negative CPU allocatable %d", in.Workers.CPUAllocatableMillicores)
	}
	if in.Workers.MemoryAllocatableBytes < 0 {
		return fmt.Errorf("hcp capacity: negative memory allocatable %d", in.Workers.MemoryAllocatableBytes)
	}
	if in.Workers.MaxPods < 0 {
		return fmt.Errorf("hcp capacity: negative max pods %d", in.Workers.MaxPods)
	}
	if in.CurrentHCCount < 0 {
		return fmt.Errorf("hcp capacity: negative current HC count %d", in.CurrentHCCount)
	}
	if in.ReadyHCPCount < 0 {
		return fmt.Errorf("hcp capacity: negative ready HC count %d", in.ReadyHCPCount)
	}
	if in.ReadyHCPQPSKnown {
		if math.IsNaN(in.AverageReadyHCPQPS) || math.IsInf(in.AverageReadyHCPQPS, 0) {
			return fmt.Errorf("hcp capacity: non-finite average QPS %v", in.AverageReadyHCPQPS)
		}
		if in.AverageReadyHCPQPS < 0 {
			return fmt.Errorf("hcp capacity: negative average QPS %v", in.AverageReadyHCPQPS)
		}
	}
	switch in.HostedAvailabilityBasis {
	case "", BasisHighlyAvailable, BasisMixed, BasisSingleReplica, BasisUnknown:
	default:
		return fmt.Errorf("hcp capacity: unknown hosted availability basis %q", in.HostedAvailabilityBasis)
	}
	return nil
}
