package csv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// SLORow is one cumulative histogram bucket snapshot from a
// ros-openshift-slo-YYYYMM.csv file (#644 canonical contract, source-split
// amendment).
//
// Columns: hc_cluster_id | window_start | window_end | verb_group
// (mutating/read/other) | le (+Inf always present) | bucket_count
// (cumulative at window end) | collected_at | source (Prometheus job label:
// kubernetes, metrics-server, metrics — one row set per job, each a coherent
// cumulative histogram).
//
// Datetime format matches container CSVs. Incomplete windows are omitted
// by the operator (absence reads as missing downstream).
type SLORow struct {
	HCClusterID string
	WindowStart time.Time
	WindowEnd   time.Time
	VerbGroup   string
	Le          float64
	BucketCount int64
	CollectedAt time.Time
	Source      string
}

// MissingSLOColumnsError lists required SLO headers that were absent.
type MissingSLOColumnsError struct {
	Columns []string
}

func (e *MissingSLOColumnsError) Error() string {
	return fmt.Sprintf("not an SLO CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

type sloColumnIndex struct {
	hcClusterID int
	windowStart int
	windowEnd   int
	verbGroup   int
	le          int
	bucketCount int
	collectedAt int
	source      int
}

func newSLOColumnIndex() sloColumnIndex {
	return sloColumnIndex{
		hcClusterID: -1, windowStart: -1, windowEnd: -1,
		verbGroup: -1, le: -1, bucketCount: -1, collectedAt: -1,
		source: -1,
	}
}

func buildSLOColumnIndex(header []string) (sloColumnIndex, error) {
	idx := newSLOColumnIndex()
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case "hc_cluster_id":
			idx.hcClusterID = i
		case "window_start":
			idx.windowStart = i
		case "window_end":
			idx.windowEnd = i
		case "verb_group":
			idx.verbGroup = i
		case "le":
			idx.le = i
		case "bucket_count":
			idx.bucketCount = i
		case "collected_at":
			idx.collectedAt = i
		case "source":
			idx.source = i
		}
	}
	var missing []string
	if idx.hcClusterID < 0 {
		missing = append(missing, "hc_cluster_id")
	}
	if idx.windowStart < 0 {
		missing = append(missing, "window_start")
	}
	if idx.windowEnd < 0 {
		missing = append(missing, "window_end")
	}
	if idx.verbGroup < 0 {
		missing = append(missing, "verb_group")
	}
	if idx.le < 0 {
		missing = append(missing, "le")
	}
	if idx.bucketCount < 0 {
		missing = append(missing, "bucket_count")
	}
	if idx.collectedAt < 0 {
		missing = append(missing, "collected_at")
	}
	if idx.source < 0 {
		missing = append(missing, "source")
	}
	if len(missing) > 0 {
		return idx, &MissingSLOColumnsError{Columns: missing}
	}
	return idx, nil
}

// validSLOVerbGroup reports whether vg is a canonical verb group.
func validSLOVerbGroup(vg string) bool {
	switch vg {
	case "mutating", "read", "other":
		return true
	default:
		return false
	}
}

func parseSLOLe(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if strings.EqualFold(s, "+inf") || strings.EqualFold(s, "inf") || s == "+Infinity" {
		return math.Inf(1), nil
	}
	return strconv.ParseFloat(s, 64)
}

// ForEachSLO parses an SLO CSV one record at a time without retaining the
// full file. Malformed rows are skipped (counted in skipped): bad timestamps,
// unknown verb groups, unparseable le/bucket_count, empty hc_cluster_id.
// Structural CSV errors still fail. ctx is checked every 10_000 accepted rows.
//
// The operator must not import this package (ADR-0305); the operator side
// owns emission, this package owns backend parsing.
func ForEachSLO(ctx context.Context, r io.Reader, fn func(SLORow) error) (skipped int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading header: %w", err)
	}
	idx, err := buildSLOColumnIndex(header)
	if err != nil {
		return 0, err
	}
	accepted := 0
	lineNum := 1
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return skipped, nil
		}
		if err != nil {
			return skipped, fmt.Errorf("reading line %d: %w", lineNum+1, err)
		}
		lineNum++
		row, parseErr := parseSLORecord(record, idx)
		if parseErr != nil {
			skipped++
			continue
		}
		if err := fn(row); err != nil {
			return skipped, err
		}
		accepted++
		if accepted%10000 == 0 {
			if err := ctx.Err(); err != nil {
				return skipped, err
			}
		}
	}
}

// ParseSLORows reads an SLO CSV into a slice. See ForEachSLO for skip semantics.
func ParseSLORows(r io.Reader) (rows []SLORow, skipped int, err error) {
	rows = make([]SLORow, 0, 64)
	skipped, err = ForEachSLO(context.Background(), r, func(row SLORow) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, skipped, nil
	}
	return rows, skipped, nil
}

func parseSLORecord(record []string, idx sloColumnIndex) (SLORow, error) {
	var row SLORow
	row.HCClusterID = strings.TrimSpace(cell(record, idx.hcClusterID))
	if row.HCClusterID == "" {
		return row, fmt.Errorf("empty hc_cluster_id")
	}
	wsRaw := strings.TrimSpace(cell(record, idx.windowStart))
	ws, err := parseFlexibleTimestamp(wsRaw)
	if err != nil {
		return row, fmt.Errorf("parse window_start %q: %w", wsRaw, err)
	}
	row.WindowStart = ws
	weRaw := strings.TrimSpace(cell(record, idx.windowEnd))
	we, err := parseFlexibleTimestamp(weRaw)
	if err != nil {
		return row, fmt.Errorf("parse window_end %q: %w", weRaw, err)
	}
	row.WindowEnd = we
	if !row.WindowEnd.After(row.WindowStart) {
		return row, fmt.Errorf("window_end %q not after window_start %q", weRaw, wsRaw)
	}
	row.VerbGroup = strings.TrimSpace(strings.ToLower(cell(record, idx.verbGroup)))
	if !validSLOVerbGroup(row.VerbGroup) {
		return row, fmt.Errorf("unknown verb_group %q", row.VerbGroup)
	}
	leRaw := strings.TrimSpace(cell(record, idx.le))
	le, err := parseSLOLe(leRaw)
	if err != nil {
		return row, fmt.Errorf("parse le %q: %w", leRaw, err)
	}
	row.Le = le
	bcRaw := strings.TrimSpace(cell(record, idx.bucketCount))
	bc, err := strconv.ParseInt(bcRaw, 10, 64)
	if err != nil || bc < 0 {
		return row, fmt.Errorf("parse bucket_count %q: must be non-negative int", bcRaw)
	}
	row.BucketCount = bc
	caRaw := strings.TrimSpace(cell(record, idx.collectedAt))
	ca, err := parseFlexibleTimestamp(caRaw)
	if err != nil {
		return row, fmt.Errorf("parse collected_at %q: %w", caRaw, err)
	}
	row.CollectedAt = ca
	row.Source = strings.TrimSpace(cell(record, idx.source))
	if row.Source == "" {
		return row, fmt.Errorf("empty source")
	}
	return row, nil
}
