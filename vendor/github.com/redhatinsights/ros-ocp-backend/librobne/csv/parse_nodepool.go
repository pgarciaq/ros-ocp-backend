package csv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// NodepoolRow is one pool snapshot from a ros-openshift-nodepool-YYYYMM.csv
// file (W3 slice, #660 Child A → #673).
//
// Columns: hc_cluster_id | pool_name | window_start | window_end |
// spec_replicas | status_replicas | autoscaling ("" or "min-max") |
// collected_at | source (provenance, e.g. hypershift).
//
// Replica counts are required: unreadable counts skip the row (never
// fabricate zeros — the backend rule interprets, this package observes).
// Datetime format matches container CSVs.
type NodepoolRow struct {
	HCClusterID    string
	PoolName       string
	WindowStart    time.Time
	WindowEnd      time.Time
	SpecReplicas   int64
	StatusReplicas int64
	Autoscaling    string
	CollectedAt    time.Time
	Source         string
}

// MissingNodepoolColumnsError lists required nodepool headers that were absent.
type MissingNodepoolColumnsError struct {
	Columns []string
}

func (e *MissingNodepoolColumnsError) Error() string {
	return fmt.Sprintf("not a NodePool CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

var nodepoolAutoscalingRE = regexp.MustCompile(`^[0-9]+-[0-9]+$`)

type nodepoolColumnIndex struct {
	hc, pool, windowSt, windowEnd int
	spec, status, auto            int
	collected, source             int
}

func buildNodepoolColumnIndex(header []string) (nodepoolColumnIndex, error) {
	idx := nodepoolColumnIndex{
		hc: -1, pool: -1, windowSt: -1, windowEnd: -1,
		spec: -1, status: -1, auto: -1, collected: -1, source: -1,
	}
	for i, h := range header {
		switch strings.TrimSpace(strings.ToLower(h)) {
		case "hc_cluster_id":
			idx.hc = i
		case "pool_name":
			idx.pool = i
		case "window_start":
			idx.windowSt = i
		case "window_end":
			idx.windowEnd = i
		case "spec_replicas":
			idx.spec = i
		case "status_replicas":
			idx.status = i
		case "autoscaling":
			idx.auto = i
		case "collected_at":
			idx.collected = i
		case "source":
			idx.source = i
		}
	}
	var missing []string
	add := func(name string, v int) {
		if v < 0 {
			missing = append(missing, name)
		}
	}
	add("hc_cluster_id", idx.hc)
	add("pool_name", idx.pool)
	add("window_start", idx.windowSt)
	add("window_end", idx.windowEnd)
	add("spec_replicas", idx.spec)
	add("status_replicas", idx.status)
	add("autoscaling", idx.auto)
	add("collected_at", idx.collected)
	add("source", idx.source)
	if len(missing) > 0 {
		return idx, &MissingNodepoolColumnsError{Columns: missing}
	}
	return idx, nil
}

func parseNodepoolCount(raw, what string) (int64, error) {
	raw = strings.TrimSpace(raw)
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("parse %s %q: must be non-negative int", what, raw)
	}
	return v, nil
}

func parseNodepoolRecord(record []string, idx nodepoolColumnIndex) (NodepoolRow, error) {
	var row NodepoolRow
	row.HCClusterID = strings.TrimSpace(cell(record, idx.hc))
	if row.HCClusterID == "" {
		return row, fmt.Errorf("empty hc_cluster_id")
	}
	row.PoolName = strings.TrimSpace(cell(record, idx.pool))
	if row.PoolName == "" {
		return row, fmt.Errorf("empty pool_name")
	}
	wsRaw := strings.TrimSpace(cell(record, idx.windowSt))
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
	if v, err := parseNodepoolCount(cell(record, idx.spec), "spec_replicas"); err != nil {
		return row, err
	} else {
		row.SpecReplicas = v
	}
	if v, err := parseNodepoolCount(cell(record, idx.status), "status_replicas"); err != nil {
		return row, err
	} else {
		row.StatusReplicas = v
	}
	row.Autoscaling = strings.TrimSpace(cell(record, idx.auto))
	if row.Autoscaling != "" && !nodepoolAutoscalingRE.MatchString(row.Autoscaling) {
		return row, fmt.Errorf("parse autoscaling %q: must be empty or min-max", row.Autoscaling)
	}
	caRaw := strings.TrimSpace(cell(record, idx.collected))
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

// ForEachNodepool parses a NodePool CSV one record at a time without
// retaining the full file. Malformed rows are skipped (counted in
// skipped); structural CSV errors still fail. ctx is checked every
// 10_000 accepted rows.
func ForEachNodepool(ctx context.Context, r io.Reader, fn func(NodepoolRow) error) (skipped int, err error) {
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
	idx, err := buildNodepoolColumnIndex(header)
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
		row, parseErr := parseNodepoolRecord(record, idx)
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

// ParseNodepoolRows reads a NodePool CSV into a slice. See
// ForEachNodepool for skip semantics.
func ParseNodepoolRows(r io.Reader) (rows []NodepoolRow, skipped int, err error) {
	rows = make([]NodepoolRow, 0, 16)
	skipped, err = ForEachNodepool(context.Background(), r, func(row NodepoolRow) error {
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
