package csv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// APITaxRow is one cumulative webhook snapshot from a
// ros-openshift-apitax-YYYYMM.csv file (thin W5, #393).
//
// Columns: webhook_name | window_start | window_end | le (+Inf always
// present) | bucket_count (cumulative at window end) | total_count |
// rejected_count | collected_at | source (Prometheus job label).
// total_count/rejected_count are denormalized onto every bucket row of that
// webhook and may be empty when the series was absent (never zero-filled).
// Datetime format matches container CSVs. Incomplete windows are omitted by
// the operator (absence reads as missing downstream).
type APITaxRow struct {
	WebhookName   string
	WindowStart   time.Time
	WindowEnd     time.Time
	Le            float64
	BucketCount   int64
	TotalCount    int64
	HasTotal      bool
	RejectedCount int64
	HasRejected   bool
	CollectedAt   time.Time
	Source        string
}

// MissingAPITaxColumnsError lists required API-tax headers that were absent.
type MissingAPITaxColumnsError struct {
	Columns []string
}

func (e *MissingAPITaxColumnsError) Error() string {
	return fmt.Sprintf("not an API tax CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

type apitaxColumnIndex struct {
	webhook   int
	windowSt  int
	windowEnd int
	le        int
	bucket    int
	total     int
	rejected  int
	collected int
	source    int
}

func buildAPITaxColumnIndex(header []string) (apitaxColumnIndex, error) {
	idx := apitaxColumnIndex{
		webhook: -1, windowSt: -1, windowEnd: -1, le: -1, bucket: -1,
		total: -1, rejected: -1, collected: -1, source: -1,
	}
	for i, h := range header {
		switch strings.TrimSpace(strings.ToLower(h)) {
		case "webhook_name":
			idx.webhook = i
		case "window_start":
			idx.windowSt = i
		case "window_end":
			idx.windowEnd = i
		case "le":
			idx.le = i
		case "bucket_count":
			idx.bucket = i
		case "total_count":
			idx.total = i
		case "rejected_count":
			idx.rejected = i
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
	add("webhook_name", idx.webhook)
	add("window_start", idx.windowSt)
	add("window_end", idx.windowEnd)
	add("le", idx.le)
	add("bucket_count", idx.bucket)
	add("total_count", idx.total)
	add("rejected_count", idx.rejected)
	add("collected_at", idx.collected)
	add("source", idx.source)
	if len(missing) > 0 {
		return idx, &MissingAPITaxColumnsError{Columns: missing}
	}
	return idx, nil
}

func parseAPITaxOptCount(raw, what string) (int64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, false, fmt.Errorf("parse %s %q: must be non-negative int or empty", what, raw)
	}
	return v, true, nil
}

func parseAPITaxRecord(record []string, idx apitaxColumnIndex) (APITaxRow, error) {
	var row APITaxRow
	row.WebhookName = strings.TrimSpace(cell(record, idx.webhook))
	if row.WebhookName == "" {
		return row, fmt.Errorf("empty webhook_name")
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
	leRaw := strings.TrimSpace(cell(record, idx.le))
	le, err := parseSLOLe(leRaw)
	if err != nil {
		return row, fmt.Errorf("parse le %q: %w", leRaw, err)
	}
	row.Le = le
	bcRaw := strings.TrimSpace(cell(record, idx.bucket))
	bc, err := strconv.ParseInt(bcRaw, 10, 64)
	if err != nil || bc < 0 {
		return row, fmt.Errorf("parse bucket_count %q: must be non-negative int", bcRaw)
	}
	row.BucketCount = bc
	if v, ok, err := parseAPITaxOptCount(cell(record, idx.total), "total_count"); err != nil {
		return row, err
	} else {
		row.TotalCount, row.HasTotal = v, ok
	}
	if v, ok, err := parseAPITaxOptCount(cell(record, idx.rejected), "rejected_count"); err != nil {
		return row, err
	} else {
		row.RejectedCount, row.HasRejected = v, ok
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

// ForEachAPITax parses an API tax CSV one record at a time without retaining
// the full file. Malformed rows are skipped (counted in skipped); structural
// CSV errors still fail. ctx is checked every 10_000 accepted rows.
func ForEachAPITax(ctx context.Context, r io.Reader, fn func(APITaxRow) error) (skipped int, err error) {
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
	idx, err := buildAPITaxColumnIndex(header)
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
		row, parseErr := parseAPITaxRecord(record, idx)
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

// ParseAPITaxRows reads an API tax CSV into a slice. See ForEachAPITax for
// skip semantics.
func ParseAPITaxRows(r io.Reader) (rows []APITaxRow, skipped int, err error) {
	rows = make([]APITaxRow, 0, 64)
	skipped, err = ForEachAPITax(context.Background(), r, func(row APITaxRow) error {
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
