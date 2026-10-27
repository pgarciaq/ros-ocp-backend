package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"time"

	hcpcapacity "github.com/redhatinsights/ros-ocp-backend/librobne/hcp"
)

// W4 fleet-capacity snapshot CSV parsing (#392 contract, shared for #700).
//
// The 25-column header is the operator-side contract (KMO
// TestHCPCapacityRow_Contract pins the same list); the model version is
// validated against librobne/hcp.ModelVersion at parse so the operator
// and the math cannot silently drift. This parser is the single source
// for backend ingestion and the robne CLI.
//
// Validation is split by clock dependence: ParseHCPCapacityRows performs
// time-INDEPENDENT validation only (Load has no clock); the future-skew
// rule lives in HCPCapacitySkewCheck, applied by consumers that know the
// time (ingestion wall clock, CLI --now). Row-level problems skip with
// reasons — never zero-filled. A header mismatch or CSV syntax error
// aborts the file parse and returns an error; the poison contract (a
// capacity file must never gate other entities) is the caller's concern.

// HCPCapacitySkewTolerance bounds the operator-supplied observed_at: a
// future-dated row cannot keep `latest` permanently fresh.
const HCPCapacitySkewTolerance = 5 * time.Minute

// HCPCapacityMaxCadenceMinutes bounds the cadence fields the staleness
// clock (2× max cadence) depends on: a zero or absurd value would make
// the window degenerate and mark every row permanently stale.
const HCPCapacityMaxCadenceMinutes = 60 * 24 * 60 // 60 days

// HCPCapacityHeader is the 25-column operator contract, in order.
var HCPCapacityHeader = []string{
	"observed_at", "mce_sampled_at", "operator_version", "ocp_version", "mce_version",
	"native_model_version", "collection_interval_minutes", "upload_cycle_minutes",
	"inventory_complete", "current_hc_count", "ready_hcp_count",
	"average_ready_hcp_qps", "ready_hcp_qps_known", "hosted_availability_basis",
	"eligible_worker_cpu_allocatable_millicores", "eligible_worker_memory_allocatable_bytes",
	"eligible_worker_max_pods", "worker_placement_known", "pressure_state",
	"mce_request_max_hcs", "mce_low_qps_max_hcs", "mce_average_qps_max_hcs",
	"mce_medium_qps_max_hcs", "mce_high_qps_max_hcs",
	// Appended (contract position 25): compact/shared-infrastructure
	// topology — management control-plane nodes doubling as workers
	// (ADR-0334 §3.2.5 caveat).
	"shared_infrastructure",
}

// HCPCapacityRow is one parsed snapshot row. cluster_uuid is deliberately
// absent: on the backend it arrives as trusted Kafka/report metadata,
// never CSV-supplied; CLI-side it comes from the payload manifest.
type HCPCapacityRow struct {
	ObservedAt                time.Time
	MCESampledAt              *time.Time
	OperatorVersion           string
	OCPVersion                string
	MCEVersion                string
	NativeModelVersion        string
	CollectionIntervalMinutes int64
	UploadCycleMinutes        int64
	InventoryComplete         bool
	CurrentHCCount            int64
	ReadyHCPCount             int64
	AverageReadyHCPQPS        float64
	ReadyHCPQPSKnown          bool
	HostedAvailabilityBasis   string
	EligibleCPUMillicores     int64
	EligibleMemoryBytes       int64
	EligibleMaxPods           int64
	WorkerPlacementKnown      bool
	PressureState             string
	SharedInfrastructure      bool
	MCERequestMaxHCs          *int64
	MCELowQPSMaxHCs           *int64
	MCEAverageQPSMaxHCs       *int64
	MCEMediumQPSMaxHCs        *int64
	MCEHighQPSMaxHCs          *int64
}

// RowSkip is one skipped data row: the 1-based index after the header,
// the observed_at when field 0 parsed (else ""), and the reason.
type RowSkip struct {
	Index      int
	ObservedAt string
	Reason     string
}

func parseStrictBool(s string) (bool, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("not a strict boolean: %q", s)
}

func parseOptionalInt64(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// parseHCPCapacityRecord validates one 25-field record (time-independent
// rules only); ok=false carries the skip reason.
func parseHCPCapacityRecord(rec []string) (HCPCapacityRow, string, bool) {
	var r HCPCapacityRow
	if len(rec) != len(HCPCapacityHeader) {
		return r, fmt.Sprintf("wrong number of columns: got %d, want %d", len(rec), len(HCPCapacityHeader)), false
	}
	var err error
	if r.ObservedAt, err = time.Parse(time.RFC3339, rec[0]); err != nil {
		return r, "observed_at is not RFC3339", false
	}
	if rec[1] != "" {
		t, err := time.Parse(time.RFC3339, rec[1])
		if err != nil {
			return r, "mce_sampled_at is not RFC3339", false
		}
		r.MCESampledAt = &t
	}
	r.OperatorVersion = rec[2]
	r.OCPVersion = rec[3]
	r.MCEVersion = rec[4]
	if rec[5] != hcpcapacity.ModelVersion {
		return r, fmt.Sprintf("native_model_version %q does not match %q", rec[5], hcpcapacity.ModelVersion), false
	}
	r.NativeModelVersion = rec[5]
	if r.CollectionIntervalMinutes, err = strconv.ParseInt(rec[6], 10, 64); err != nil {
		return r, "collection_interval_minutes is not an integer", false
	}
	if r.UploadCycleMinutes, err = strconv.ParseInt(rec[7], 10, 64); err != nil {
		return r, "upload_cycle_minutes is not an integer", false
	}
	// Cadence bounds: the staleness window is 2× max(cadence); zero or
	// absurd values would degenerate it (every row permanently stale, or
	// a negative window after Duration overflow).
	if r.CollectionIntervalMinutes <= 0 || r.CollectionIntervalMinutes > HCPCapacityMaxCadenceMinutes ||
		r.UploadCycleMinutes <= 0 || r.UploadCycleMinutes > HCPCapacityMaxCadenceMinutes {
		return r, "cadence out of bounds (must be 1..86400 minutes)", false
	}
	if r.InventoryComplete, err = parseStrictBool(rec[8]); err != nil {
		return r, "inventory_complete is not a strict boolean", false
	}
	if r.CurrentHCCount, err = strconv.ParseInt(rec[9], 10, 64); err != nil {
		return r, "current_hc_count is not an integer", false
	}
	if r.ReadyHCPCount, err = strconv.ParseInt(rec[10], 10, 64); err != nil {
		return r, "ready_hcp_count is not an integer", false
	}
	if r.ReadyHCPQPSKnown, err = parseStrictBool(rec[12]); err != nil {
		return r, "ready_hcp_qps_known is not a strict boolean", false
	}
	if r.ReadyHCPQPSKnown {
		q, err := strconv.ParseFloat(rec[11], 64)
		if err != nil || math.IsNaN(q) || math.IsInf(q, 0) || q < 0 {
			return r, "average_ready_hcp_qps is not a non-negative finite number", false
		}
		r.AverageReadyHCPQPS = q
	} else if rec[11] != "" {
		return r, "average_ready_hcp_qps must be empty when ready_hcp_qps_known is false", false
	}
	r.HostedAvailabilityBasis = rec[13]
	switch r.HostedAvailabilityBasis {
	case "", hcpcapacity.BasisHighlyAvailable, hcpcapacity.BasisMixed, hcpcapacity.BasisSingleReplica, hcpcapacity.BasisUnknown:
	default:
		return r, fmt.Sprintf("hosted_availability_basis %q is not a known enum value", rec[13]), false
	}
	if r.EligibleCPUMillicores, err = strconv.ParseInt(rec[14], 10, 64); err != nil {
		return r, "eligible_worker_cpu_allocatable_millicores is not an integer", false
	}
	if r.EligibleMemoryBytes, err = strconv.ParseInt(rec[15], 10, 64); err != nil {
		return r, "eligible_worker_memory_allocatable_bytes is not an integer", false
	}
	if r.EligibleMaxPods, err = strconv.ParseInt(rec[16], 10, 64); err != nil {
		return r, "eligible_worker_max_pods is not an integer", false
	}
	if r.WorkerPlacementKnown, err = parseStrictBool(rec[17]); err != nil {
		return r, "worker_placement_known is not a strict boolean", false
	}
	switch rec[18] {
	case "pressured", "unknown":
		r.PressureState = rec[18]
	default:
		return r, fmt.Sprintf("pressure_state %q is not pressured/unknown (v1 never claims clear)", rec[18]), false
	}
	if r.SharedInfrastructure, err = parseStrictBool(rec[24]); err != nil {
		return r, "shared_infrastructure is not a strict boolean", false
	}
	if r.MCERequestMaxHCs, err = parseOptionalInt64(rec[19]); err != nil {
		return r, "mce_request_max_hcs is not an integer", false
	}
	if r.MCELowQPSMaxHCs, err = parseOptionalInt64(rec[20]); err != nil {
		return r, "mce_low_qps_max_hcs is not an integer", false
	}
	if r.MCEAverageQPSMaxHCs, err = parseOptionalInt64(rec[21]); err != nil {
		return r, "mce_average_qps_max_hcs is not an integer", false
	}
	if r.MCEMediumQPSMaxHCs, err = parseOptionalInt64(rec[22]); err != nil {
		return r, "mce_medium_qps_max_hcs is not an integer", false
	}
	if r.MCEHighQPSMaxHCs, err = parseOptionalInt64(rec[23]); err != nil {
		return r, "mce_high_qps_max_hcs is not an integer", false
	}
	return r, "", true
}

// ParseHCPCapacityRows parses the snapshot CSV with time-INDEPENDENT
// validation only (no skew check — Load has no clock). Header mismatch or
// a CSV syntax error aborts and returns an error; row-level problems skip
// with reasons.
func ParseHCPCapacityRows(r io.Reader) (rows []HCPCapacityRow, skips []RowSkip, err error) {
	reader := csv.NewReader(r)
	// Variable field count: a short or long row is a row-level problem
	// (skip with reason), not a file abort — the length check in
	// parseHCPCapacityRecord governs.
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("hcp-capacity: reading header: %w", err)
	}
	if !reflect.DeepEqual(header, HCPCapacityHeader) {
		return nil, nil, fmt.Errorf("hcp-capacity: missing or unexpected columns (header must match the 25-column operator contract)")
	}
	index := 0
	for {
		rec, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("hcp-capacity: reading row: %w", err)
		}
		index++
		row, reason, ok := parseHCPCapacityRecord(rec)
		if !ok {
			skips = append(skips, RowSkip{Index: index, ObservedAt: rec[0], Reason: reason})
			continue
		}
		rows = append(rows, row)
	}
	return rows, skips, nil
}

// HCPCapacitySkewCheck applies the future-skew rule (>5 minutes ahead of
// now drops): the single source for both ingestion (wall clock) and the
// CLI (the capacity clock). Exactly-at-tolerance is kept (strictly After).
func HCPCapacitySkewCheck(rows []HCPCapacityRow, now time.Time) (kept []HCPCapacityRow, dropped []RowSkip) {
	kept = make([]HCPCapacityRow, 0, len(rows))
	for i, r := range rows {
		if r.ObservedAt.After(now.Add(HCPCapacitySkewTolerance)) {
			dropped = append(dropped, RowSkip{
				Index:      i + 1,
				ObservedAt: r.ObservedAt.Format(time.RFC3339),
				Reason:     fmt.Sprintf("future-dated beyond the %s tolerance (cluster clock skew?)", HCPCapacitySkewTolerance),
			})
			continue
		}
		kept = append(kept, r)
	}
	return kept, dropped
}
