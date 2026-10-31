package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
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
func parseHCPCapacityRecord(rec []string, idx hcpCapacityColumnIndex, want int) (HCPCapacityRow, string, bool) {
	var r HCPCapacityRow
	if len(rec) != want {
		return r, fmt.Sprintf("wrong number of columns: got %d, want %d", len(rec), want), false
	}
	var err error
	if r.ObservedAt, err = time.Parse(time.RFC3339, rec[idx.observedAt]); err != nil {
		return r, "observed_at is not RFC3339", false
	}
	if rec[idx.mceSampledAt] != "" {
		t, err := time.Parse(time.RFC3339, rec[idx.mceSampledAt])
		if err != nil {
			return r, "mce_sampled_at is not RFC3339", false
		}
		r.MCESampledAt = &t
	}
	r.OperatorVersion = rec[idx.operatorVersion]
	r.OCPVersion = rec[idx.ocpVersion]
	r.MCEVersion = rec[idx.mceVersion]
	if rec[idx.nativeModelVersion] != hcpcapacity.ModelVersion {
		return r, fmt.Sprintf("native_model_version %q does not match %q", rec[idx.nativeModelVersion], hcpcapacity.ModelVersion), false
	}
	r.NativeModelVersion = rec[idx.nativeModelVersion]
	if r.CollectionIntervalMinutes, err = strconv.ParseInt(rec[idx.collectionIntervalMinutes], 10, 64); err != nil {
		return r, "collection_interval_minutes is not an integer", false
	}
	if r.UploadCycleMinutes, err = strconv.ParseInt(rec[idx.uploadCycleMinutes], 10, 64); err != nil {
		return r, "upload_cycle_minutes is not an integer", false
	}
	// Cadence bounds: the staleness window is 2× max(cadence); zero or
	// absurd values would degenerate it (every row permanently stale, or
	// a negative window after Duration overflow).
	if r.CollectionIntervalMinutes <= 0 || r.CollectionIntervalMinutes > HCPCapacityMaxCadenceMinutes ||
		r.UploadCycleMinutes <= 0 || r.UploadCycleMinutes > HCPCapacityMaxCadenceMinutes {
		return r, "cadence out of bounds (must be 1..86400 minutes)", false
	}
	if r.InventoryComplete, err = parseStrictBool(rec[idx.inventoryComplete]); err != nil {
		return r, "inventory_complete is not a strict boolean", false
	}
	if r.CurrentHCCount, err = strconv.ParseInt(rec[idx.currentHCCount], 10, 64); err != nil {
		return r, "current_hc_count is not an integer", false
	}
	if r.ReadyHCPCount, err = strconv.ParseInt(rec[idx.readyHCPCount], 10, 64); err != nil {
		return r, "ready_hcp_count is not an integer", false
	}
	if r.ReadyHCPQPSKnown, err = parseStrictBool(rec[idx.readyHCPQPSKnown]); err != nil {
		return r, "ready_hcp_qps_known is not a strict boolean", false
	}
	if r.ReadyHCPQPSKnown {
		q, err := strconv.ParseFloat(rec[idx.averageReadyHCPQPS], 64)
		if err != nil || math.IsNaN(q) || math.IsInf(q, 0) || q < 0 {
			return r, "average_ready_hcp_qps is not a non-negative finite number", false
		}
		r.AverageReadyHCPQPS = q
	} else if rec[idx.averageReadyHCPQPS] != "" {
		return r, "average_ready_hcp_qps must be empty when ready_hcp_qps_known is false", false
	}
	r.HostedAvailabilityBasis = rec[idx.hostedAvailabilityBasis]
	switch r.HostedAvailabilityBasis {
	case "", hcpcapacity.BasisHighlyAvailable, hcpcapacity.BasisMixed, hcpcapacity.BasisSingleReplica, hcpcapacity.BasisUnknown:
	default:
		return r, fmt.Sprintf("hosted_availability_basis %q is not a known enum value", rec[idx.hostedAvailabilityBasis]), false
	}
	if r.EligibleCPUMillicores, err = strconv.ParseInt(rec[idx.eligibleCPUMillicores], 10, 64); err != nil {
		return r, "eligible_worker_cpu_allocatable_millicores is not an integer", false
	}
	if r.EligibleMemoryBytes, err = strconv.ParseInt(rec[idx.eligibleMemoryBytes], 10, 64); err != nil {
		return r, "eligible_worker_memory_allocatable_bytes is not an integer", false
	}
	if r.EligibleMaxPods, err = strconv.ParseInt(rec[idx.eligibleMaxPods], 10, 64); err != nil {
		return r, "eligible_worker_max_pods is not an integer", false
	}
	if r.WorkerPlacementKnown, err = parseStrictBool(rec[idx.workerPlacementKnown]); err != nil {
		return r, "worker_placement_known is not a strict boolean", false
	}
	switch rec[idx.pressureState] {
	case "pressured", "unknown":
		r.PressureState = rec[idx.pressureState]
	default:
		return r, fmt.Sprintf("pressure_state %q is not pressured/unknown (v1 never claims clear)", rec[idx.pressureState]), false
	}
	if r.SharedInfrastructure, err = parseStrictBool(rec[idx.sharedInfrastructure]); err != nil {
		return r, "shared_infrastructure is not a strict boolean", false
	}
	if r.MCERequestMaxHCs, err = parseOptionalInt64(rec[idx.mceRequestMaxHCs]); err != nil {
		return r, "mce_request_max_hcs is not an integer", false
	}
	if r.MCELowQPSMaxHCs, err = parseOptionalInt64(rec[idx.mceLowQPSMaxHCs]); err != nil {
		return r, "mce_low_qps_max_hcs is not an integer", false
	}
	if r.MCEAverageQPSMaxHCs, err = parseOptionalInt64(rec[idx.mceAverageQPSMaxHCs]); err != nil {
		return r, "mce_average_qps_max_hcs is not an integer", false
	}
	if r.MCEMediumQPSMaxHCs, err = parseOptionalInt64(rec[idx.mceMediumQPSMaxHCs]); err != nil {
		return r, "mce_medium_qps_max_hcs is not an integer", false
	}
	if r.MCEHighQPSMaxHCs, err = parseOptionalInt64(rec[idx.mceHighQPSMaxHCs]); err != nil {
		return r, "mce_high_qps_max_hcs is not an integer", false
	}
	return r, "", true
}

// ParseHCPCapacityRows parses the snapshot CSV with time-INDEPENDENT
// validation only (no skew check — Load has no clock). Header mismatch or
// a CSV syntax error aborts and returns an error; row-level problems skip
// with reasons.
// hcpCapacityColumnIndex locates each contract column by header name
// (#704 option 3: the family convention — name-based, order-insensitive).
type hcpCapacityColumnIndex struct {
	observedAt, mceSampledAt, operatorVersion, ocpVersion, mceVersion,
	nativeModelVersion, collectionIntervalMinutes, uploadCycleMinutes,
	inventoryComplete, currentHCCount, readyHCPCount, averageReadyHCPQPS,
	readyHCPQPSKnown, hostedAvailabilityBasis, eligibleCPUMillicores,
	eligibleMemoryBytes, eligibleMaxPods, workerPlacementKnown, pressureState,
	mceRequestMaxHCs, mceLowQPSMaxHCs, mceAverageQPSMaxHCs, mceMediumQPSMaxHCs,
	mceHighQPSMaxHCs, sharedInfrastructure int
}

// buildHCPCapacityColumnIndex maps header names to record positions
// (case-insensitive, trimmed; duplicate names last-wins, mirroring the
// snapshot parser). Returns the missing required names and the
// unrecognized extra names: extras are tolerated (reported for the caller
// to warn); missing names are the caller's file-level error.
func buildHCPCapacityColumnIndex(header []string) (idx hcpCapacityColumnIndex, missing, extras []string) {
	fields := [...](*int){
		&idx.observedAt, &idx.mceSampledAt, &idx.operatorVersion, &idx.ocpVersion, &idx.mceVersion,
		&idx.nativeModelVersion, &idx.collectionIntervalMinutes, &idx.uploadCycleMinutes,
		&idx.inventoryComplete, &idx.currentHCCount, &idx.readyHCPCount, &idx.averageReadyHCPQPS,
		&idx.readyHCPQPSKnown, &idx.hostedAvailabilityBasis, &idx.eligibleCPUMillicores,
		&idx.eligibleMemoryBytes, &idx.eligibleMaxPods, &idx.workerPlacementKnown, &idx.pressureState,
		&idx.mceRequestMaxHCs, &idx.mceLowQPSMaxHCs, &idx.mceAverageQPSMaxHCs, &idx.mceMediumQPSMaxHCs,
		&idx.mceHighQPSMaxHCs, &idx.sharedInfrastructure,
	}
	byName := make(map[string]*int, len(HCPCapacityHeader))
	for i, name := range HCPCapacityHeader {
		byName[name] = fields[i]
	}
	seen := make(map[string]bool, len(header))
	for i, col := range header {
		name := strings.ToLower(strings.TrimSpace(col))
		f, ok := byName[name]
		if !ok {
			extras = append(extras, name)
			continue
		}
		*f = i
		seen[name] = true
	}
	for _, name := range HCPCapacityHeader {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	return idx, missing, extras
}

// ParseHCPCapacityRows parses the snapshot CSV with time-INDEPENDENT
// validation only. Name-based (#704 option 3): column order does not
// matter, extra columns are tolerated and reported in extras (callers
// warn), and missing required names are a file-level error naming them.
func ParseHCPCapacityRows(r io.Reader) (rows []HCPCapacityRow, skips []RowSkip, extras []string, err error) {
	reader := csv.NewReader(r)
	// Variable field count: a short or long row is a row-level problem
	// (skip with reason), not a file abort — the length check in
	// parseHCPCapacityRecord governs.
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("hcp-capacity: reading header: %w", err)
	}
	idx, missing, extras := buildHCPCapacityColumnIndex(header)
	if len(missing) > 0 {
		return nil, nil, extras, fmt.Errorf("hcp-capacity: missing columns: %s (header must carry every name in the 25-column operator contract)", strings.Join(missing, ", "))
	}
	index := 0
	for {
		rec, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("hcp-capacity: reading row: %w", err)
		}
		index++
		row, reason, ok := parseHCPCapacityRecord(rec, idx, len(header))
		if !ok {
			skips = append(skips, RowSkip{Index: index, ObservedAt: rec[idx.observedAt], Reason: reason})
			continue
		}
		rows = append(rows, row)
	}
	return rows, skips, extras, nil
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
