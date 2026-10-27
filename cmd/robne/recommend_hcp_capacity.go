package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	roscsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	hcpcapacity "github.com/redhatinsights/ros-ocp-backend/librobne/hcp"
)

// W4 fleet-capacity compute path (#700 Task 4). The CSV parser and the
// serve-time derivation rules are shared (librobne/csv, librobne/hcp);
// this file assembles the in-payload series, computes each observation
// via CalculateCapacity, and maps into the CLI DTO.

// resolveCapacityNow is the capacity clock: --now > YAML now > wall.
// Mirrors parseNow's flag/YAML dialects (RFC3339, then RFC3339Nano; the
// same "--now:"/"yaml now:" error prefixes — round-9 W3). Never derives
// from interval_end (capacity rows have none) and never from observed_at
// (would pin the latest sample age to zero, so nothing is ever stale).
// A bad YAML now is a hard error, same as parseNow.
func resolveCapacityNow(flag string, cfg fileConfig) (time.Time, error) {
	if flag != "" {
		t, err := time.Parse(time.RFC3339, flag)
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, flag)
			if err != nil {
				return time.Time{}, fmt.Errorf("--now: %w", err)
			}
		}
		return t.UTC(), nil
	}
	if cfg.Now != nil && strings.TrimSpace(*cfg.Now) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*cfg.Now))
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(*cfg.Now))
			if err != nil {
				return time.Time{}, fmt.Errorf("yaml now: %w", err)
			}
		}
		return t.UTC(), nil
	}
	return time.Now().UTC(), nil
}

// assembleCapacitySeries builds the newest-first series: skew-check →
// stable sort newest-first → dedup by observed_at keeping the LAST of
// tied group in read order (a stable sort preserves read order among
// observed_at ties; keeping the first would select the stale row of a
// recollected window — backend last-write-wins convergence, round-8 W1).
// Latest = series[0], independent of --now (age surfaces via the
// staleness warning).
func assembleCapacitySeries(rows []roscsv.HCPCapacityRow, now time.Time) (series []roscsv.HCPCapacityRow, skewDrops []roscsv.RowSkip) {
	kept, skewDrops := roscsv.HCPCapacitySkewCheck(rows, now)
	sort.SliceStable(kept, func(i, j int) bool {
		return kept[i].ObservedAt.After(kept[j].ObservedAt)
	})
	// Ties on observed_at are adjacent post-sort and preserve read order;
	// walk groups and keep each group's last element.
	out := make([]roscsv.HCPCapacityRow, 0, len(kept))
	for i := 0; i < len(kept); i++ {
		j := i
		for j+1 < len(kept) && kept[j+1].ObservedAt.Equal(kept[i].ObservedAt) {
			j++
		}
		out = append(out, kept[j])
		i = j
	}
	return out, skewDrops
}

// capacityRowToServe computes the five scenarios for one row and maps it
// into the shared derivation view (the DTO build reads the view and the
// CapacityResult).
func capacityRowToServe(row roscsv.HCPCapacityRow) (hcpcapacity.ServeObservation, hcpcapacity.CapacityResult, error) {
	res, err := hcpcapacity.CalculateCapacity(hcpcapacity.CapacityInputs{
		Workers: hcpcapacity.WorkerCapacity{
			CPUAllocatableMillicores: row.EligibleCPUMillicores,
			MemoryAllocatableBytes:   row.EligibleMemoryBytes,
			MaxPods:                  row.EligibleMaxPods,
			PlacementKnown:           row.WorkerPlacementKnown,
		},
		CurrentHCCount:          row.CurrentHCCount,
		InventoryComplete:       row.InventoryComplete,
		ReadyHCPCount:           row.ReadyHCPCount,
		AverageReadyHCPQPS:      row.AverageReadyHCPQPS,
		ReadyHCPQPSKnown:        row.ReadyHCPQPSKnown,
		HostedAvailabilityBasis: row.HostedAvailabilityBasis,
	})
	if err != nil {
		return hcpcapacity.ServeObservation{}, res, fmt.Errorf("hcp-capacity: scenario calculation for %s: %w", row.ObservedAt.Format(time.RFC3339), err)
	}
	var mceSampled *time.Time
	if row.MCESampledAt != nil {
		utc := row.MCESampledAt.UTC()
		mceSampled = &utc
	}
	qps := row.AverageReadyHCPQPS
	var qpsPtr *float64
	if row.ReadyHCPQPSKnown {
		qpsPtr = &qps
	}
	obs := hcpcapacity.ServeObservation{
		ObservedAt:           row.ObservedAt.UTC(),
		MCESampledAt:         mceSampled,
		OperatorVersion:      row.OperatorVersion,
		OCPVersion:           row.OCPVersion,
		MCEVersion:           row.MCEVersion,
		NativeModelVersion:   row.NativeModelVersion,
		CollectionMinutes:    row.CollectionIntervalMinutes,
		UploadMinutes:        row.UploadCycleMinutes,
		InventoryComplete:    row.InventoryComplete,
		QPSKnown:             row.ReadyHCPQPSKnown,
		QPS:                  qpsPtr,
		CurrentHC:            row.CurrentHCCount,
		ReadyHC:              row.ReadyHCPCount,
		Basis:                row.HostedAvailabilityBasis,
		Pressure:             row.PressureState,
		SharedInfrastructure: row.SharedInfrastructure,
		Workers: hcpcapacity.WorkerCapacity{
			CPUAllocatableMillicores: row.EligibleCPUMillicores,
			MemoryAllocatableBytes:   row.EligibleMemoryBytes,
			MaxPods:                  row.EligibleMaxPods,
			PlacementKnown:           row.WorkerPlacementKnown,
		},
		MCERequest: row.MCERequestMaxHCs,
		MCELow:     row.MCELowQPSMaxHCs,
		MCEAverage: row.MCEAverageQPSMaxHCs,
		MCEMedium:  row.MCEMediumQPSMaxHCs,
		MCEHigh:    row.MCEHighQPSMaxHCs,
		Scenarios:  res.Scenarios,
	}
	return obs, res, nil
}

// computeHCPCapacity assembles the series, computes every observation,
// and derives the serve-time shape via the shared rules. The MCE
// may-be-stale caution runs over the in-payload series (the API runs it
// over its DB window query — same rule, single source). When hcp is
// explicit and every loaded row was skew-dropped, the clock-skew
// diagnosis error fires — never "no capacity snapshot" (round-9 W2; the
// zero-rows case is requireExplicitFilePlugins', which runs pre-compute).
func computeHCPCapacity(fl fileLoad, now time.Time, clusterID string) ([]HCPCapacityOut, error) {
	series, skewDrops := assembleCapacitySeries(fl.capacityRows, now)
	if len(series) == 0 && fl.pluginsExplicit && len(skewDrops) > 0 {
		return nil, fmt.Errorf("all %d capacity rows were rejected as future-dated beyond the %s tolerance (cluster clock skew?)", len(skewDrops), roscsv.HCPCapacitySkewTolerance)
	}

	// Shared-series caution first: map each row to the view for the rule.
	obs := make([]hcpcapacity.ServeObservation, 0, len(series))
	for _, row := range series {
		o, _, err := capacityRowToServe(row)
		if err != nil {
			return nil, err
		}
		obs = append(obs, o)
	}
	var caution *bool
	if len(obs) > 0 {
		c := hcpcapacity.MCEMayBeStaleFromSeries(obs)
		caution = &c
	}

	label := clusterID
	if label == "" {
		label = "unknown"
	}
	out := make([]HCPCapacityOut, 0, len(series))
	for _, o := range obs {
		warnings := hcpcapacity.DeriveWarnings(o, now, caution)
		scenarios := make([]hcpCapacityScenarioOut, 0, len(o.Scenarios))
		for _, sc := range o.Scenarios {
			so := hcpCapacityScenarioOut{
				Name:             sc.Name,
				MaxHCs:           sc.MaxHCs,
				LimitingResource: sc.LimitingResource,
				Status:           hcpcapacity.ScenarioStatus(o.Basis),
			}
			if h := hcpcapacity.DeriveHeadroom(o, sc.Name, sc.MaxHCs, sc.LimitingResource); h != nil {
				so.Headroom = h
			}
			scenarios = append(scenarios, so)
		}
		age := now.Sub(o.ObservedAt)
		if age < 0 {
			age = 0
		}
		out = append(out, HCPCapacityOut{
			ClusterUUID:               label,
			ObservedAt:                o.ObservedAt,
			MCESampledAt:              o.MCESampledAt,
			NativeModelVersion:        o.NativeModelVersion,
			SourceVersions:            hcpCapacitySourceVersionsOut{Operator: o.OperatorVersion, OCP: o.OCPVersion, MCE: o.MCEVersion},
			CollectionIntervalMinutes: o.CollectionMinutes,
			UploadCycleMinutes:        o.UploadMinutes,
			SampleAgeSeconds:          int64(age.Seconds()),
			InventoryComplete:         o.InventoryComplete,
			WorkerPlacementKnown:      o.Workers.PlacementKnown,
			CurrentHCCount:            o.CurrentHC,
			ReadyHCPCount:             o.ReadyHC,
			AverageReadyHCPQPS:        o.QPS,
			ReadyHCPQPSKnown:          o.QPSKnown,
			HostedAvailabilityBasis:   o.Basis,
			PressureState:             o.Pressure,
			EligibleWorkers: hcpCapacityEligibleWorkersOut{
				CPUAllocatableMillicores: o.Workers.CPUAllocatableMillicores,
				MemoryAllocatableBytes:   o.Workers.MemoryAllocatableBytes,
				MaxPods:                  o.Workers.MaxPods,
			},
			Scenarios: scenarios,
			MCEReferences: map[string]*int64{
				hcpcapacity.ScenarioRequest:    o.MCERequest,
				hcpcapacity.ScenarioLowQPS:     o.MCELow,
				hcpcapacity.ScenarioAverageQPS: o.MCEAverage,
				hcpcapacity.ScenarioMediumQPS:  o.MCEMedium,
				hcpcapacity.ScenarioHighQPS:    o.MCEHigh,
			},
			Warnings: hcpCapacityWarningsOut{
				Stale:                warnings.Stale,
				BelowVersionFloor:    warnings.BelowVersionFloor,
				Pressured:            warnings.Pressured,
				PressureUnknown:      warnings.PressureUnknown,
				MCEMayBeStale:        warnings.MCEMayBeStale,
				SharedInfrastructure: warnings.SharedInfrastructure,
			},
		})
	}
	return out, nil
}

// printLoadWarnings surfaces file-level poison to stderr right after
// Load (spec-locked, round-9 W4) — a poisoned capacity file warns and
// the run continues.
func printLoadWarnings(loaded roscsv.LoadResult) {
	for _, w := range loaded.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", w.File, w.Reason)
	}
}
