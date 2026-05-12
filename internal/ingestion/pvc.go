package ingestion

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

// PVCRow represents a single parsed row from the storage CSV.
type PVCRow struct {
	IntervalStart         time.Time
	IntervalEnd           time.Time
	Namespace             string
	PersistentVolumeClaim string
	PersistentVolume      string
	StorageClass          string
	CapacityBytes         int64
	RequestByteSeconds    int64
	UsageByteSeconds      int64
}

type pvcHeaderIdx struct {
	intervalStart              int
	intervalEnd                int
	namespace                  int
	persistentvolumeclaim      int
	persistentvolume           int
	storageclass               int
	capacityBytes              int
	capacityByteSeconds        int
	requestByteSeconds         int
	usageByteSeconds           int
}

func newPVCHeaderIdx() pvcHeaderIdx {
	return pvcHeaderIdx{
		intervalStart:         -1,
		intervalEnd:           -1,
		namespace:             -1,
		persistentvolumeclaim: -1,
		persistentvolume:      -1,
		storageclass:          -1,
		capacityBytes:         -1,
		capacityByteSeconds:   -1,
		requestByteSeconds:    -1,
		usageByteSeconds:      -1,
	}
}

// ParsePVCRows parses the storage CSV into PVCRow structs.
func ParsePVCRows(r io.Reader) ([]PVCRow, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading PVC CSV header: %w", err)
	}

	idx := newPVCHeaderIdx()
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "namespace":
			idx.namespace = i
		case "persistentvolumeclaim":
			idx.persistentvolumeclaim = i
		case "persistentvolume":
			idx.persistentvolume = i
		case "storageclass":
			idx.storageclass = i
		case "persistentvolumeclaim_capacity_bytes":
			idx.capacityBytes = i
		case "persistentvolumeclaim_capacity_byte_seconds":
			idx.capacityByteSeconds = i
		case "volume_request_storage_byte_seconds":
			idx.requestByteSeconds = i
		case "persistentvolumeclaim_usage_byte_seconds":
			idx.usageByteSeconds = i
		}
	}

	if idx.intervalStart < 0 || idx.namespace < 0 || idx.persistentvolumeclaim < 0 {
		return nil, fmt.Errorf("PVC CSV missing required columns (interval_start, namespace, persistentvolumeclaim)")
	}

	var rows []PVCRow
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading PVC CSV row: %w", err)
		}

		row, parseErr := parsePVCRecord(record, idx)
		if parseErr != nil {
			log.Debugf("skipping PVC row: %v", parseErr)
			continue
		}
		if row.PersistentVolumeClaim == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parsePVCRecord(record []string, idx pvcHeaderIdx) (PVCRow, error) {
	var row PVCRow
	var err error

	if idx.intervalStart >= 0 && idx.intervalStart < len(record) {
		row.IntervalStart, err = parseFlexibleTimestamp(strings.TrimSpace(record[idx.intervalStart]))
		if err != nil {
			return row, fmt.Errorf("parse interval_start: %w", err)
		}
	}
	if idx.intervalEnd >= 0 && idx.intervalEnd < len(record) {
		row.IntervalEnd, _ = parseFlexibleTimestamp(strings.TrimSpace(record[idx.intervalEnd]))
	}
	if idx.namespace >= 0 && idx.namespace < len(record) {
		row.Namespace = strings.TrimSpace(record[idx.namespace])
	}
	if idx.persistentvolumeclaim >= 0 && idx.persistentvolumeclaim < len(record) {
		row.PersistentVolumeClaim = strings.TrimSpace(record[idx.persistentvolumeclaim])
	}
	if idx.persistentvolume >= 0 && idx.persistentvolume < len(record) {
		row.PersistentVolume = strings.TrimSpace(record[idx.persistentvolume])
	}
	if idx.storageclass >= 0 && idx.storageclass < len(record) {
		row.StorageClass = strings.TrimSpace(record[idx.storageclass])
	}
	if idx.capacityBytes >= 0 && idx.capacityBytes < len(record) {
		row.CapacityBytes = parseIntOrByteSeconds(record[idx.capacityBytes])
	}
	if row.CapacityBytes == 0 && idx.capacityByteSeconds >= 0 && idx.capacityByteSeconds < len(record) {
		row.CapacityBytes = parseIntOrByteSeconds(record[idx.capacityByteSeconds])
	}
	if idx.requestByteSeconds >= 0 && idx.requestByteSeconds < len(record) {
		row.RequestByteSeconds = parseIntOrByteSeconds(record[idx.requestByteSeconds])
	}
	if idx.usageByteSeconds >= 0 && idx.usageByteSeconds < len(record) {
		row.UsageByteSeconds = parseIntOrByteSeconds(record[idx.usageByteSeconds])
	}
	return row, nil
}

// parseFlexibleTimestamp handles the various timestamp formats produced
// by koku-metrics-operator and Nise:
//   - "2006-01-02 15:04:05 +0000 UTC"  (operator & Nise)
//   - "2006-01-02 15:04:05+00:00"      (alternative)
//   - time.RFC3339                       ("2006-01-02T15:04:05Z07:00")
func parseFlexibleTimestamp(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05 +0000 UTC",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05+00:00",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %q", s)
}

func parseIntOrByteSeconds(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(f)
}

// pvcDigestKey groups PVC rows by day + PVC identity.
type pvcDigestKey struct {
	Date      time.Time
	Namespace string
	PVC       string
}

// PVCDigestResult is a daily aggregated PVC digest.
type PVCDigestResult struct {
	BucketDate    time.Time
	Namespace     string
	PVC           string
	PV            string
	StorageClass  string
	CapacityBytes int64
	RequestBytes  int64
	UsageBytesMin int64
	UsageBytesMax int64
	UsageBytesAvg int64
	SampleCount   int
}

// ComputePVCDigests aggregates PVC rows into daily digests.
// The storage CSV uses byte-seconds; we convert to bytes by dividing
// by the interval duration (3600 seconds for hourly intervals).
func ComputePVCDigests(rows []PVCRow) []PVCDigestResult {
	type accumulator struct {
		pv           string
		storageClass string
		capacity     int64
		request      int64
		usageSum     int64
		usageMin     int64
		usageMax     int64
		count        int
	}

	groups := make(map[pvcDigestKey]*accumulator)

	for _, r := range rows {
		date := r.IntervalStart.Truncate(24 * time.Hour)
		key := pvcDigestKey{Date: date, Namespace: r.Namespace, PVC: r.PersistentVolumeClaim}

		intervalSeconds := r.IntervalEnd.Sub(r.IntervalStart).Seconds()
		if intervalSeconds <= 0 {
			intervalSeconds = 3600
		}

		// Convert byte-seconds to bytes for capacity and usage
		capacityBytes := r.CapacityBytes
		if r.UsageByteSeconds > 0 && capacityBytes > 1e12 {
			// If capacity looks like byte-seconds, convert
			capacityBytes = int64(float64(capacityBytes) / intervalSeconds)
		}
		usageBytes := int64(float64(r.UsageByteSeconds) / intervalSeconds)
		requestBytes := int64(float64(r.RequestByteSeconds) / intervalSeconds)

		acc, ok := groups[key]
		if !ok {
			acc = &accumulator{
				pv:           r.PersistentVolume,
				storageClass: r.StorageClass,
				capacity:     capacityBytes,
				request:      requestBytes,
				usageMin:     usageBytes,
				usageMax:     usageBytes,
			}
			groups[key] = acc
		}

		if capacityBytes > acc.capacity {
			acc.capacity = capacityBytes
		}
		if requestBytes > acc.request {
			acc.request = requestBytes
		}
		if usageBytes < acc.usageMin {
			acc.usageMin = usageBytes
		}
		if usageBytes > acc.usageMax {
			acc.usageMax = usageBytes
		}
		acc.usageSum += usageBytes
		acc.count++
		if r.PersistentVolume != "" {
			acc.pv = r.PersistentVolume
		}
		if r.StorageClass != "" {
			acc.storageClass = r.StorageClass
		}
	}

	results := make([]PVCDigestResult, 0, len(groups))
	for key, acc := range groups {
		avg := acc.usageSum
		if acc.count > 0 {
			avg = acc.usageSum / int64(acc.count)
		}
		results = append(results, PVCDigestResult{
			BucketDate:    key.Date,
			Namespace:     key.Namespace,
			PVC:           key.PVC,
			PV:            acc.pv,
			StorageClass:  acc.storageClass,
			CapacityBytes: acc.capacity,
			RequestBytes:  acc.request,
			UsageBytesMin: acc.usageMin,
			UsageBytesMax: acc.usageMax,
			UsageBytesAvg: avg,
			SampleCount:   acc.count,
		})
	}
	return results
}

// EnsurePVCDigestPartitions creates monthly partitions of daily_pvc_digests
// for all months present in the digests slice (non-fatal on error).
func EnsurePVCDigestPartitions(ctx context.Context, pool *pgxpool.Pool, digests []PVCDigestResult) {
	months := map[time.Time]struct{}{}
	for _, d := range digests {
		monthStart := time.Date(d.BucketDate.Year(), d.BucketDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	for monthStart := range months {
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("daily_pvc_digests_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_pvc_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			log.Warnf("EnsurePVCDigestPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

// UpsertPVCDigests writes daily PVC digests to the database.
func UpsertPVCDigests(ctx context.Context, pool *pgxpool.Pool, digests []PVCDigestResult, orgID, clusterUUID string) error {
	if len(digests) == 0 {
		return nil
	}

	for _, d := range digests {
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_pvc_digests (
				bucket_date, org_id, cluster_uuid, namespace,
				persistentvolumeclaim, persistentvolume, storageclass,
				capacity_bytes, request_bytes,
				usage_bytes_min, usage_bytes_max, usage_bytes_avg,
				sample_count
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (cluster_uuid, namespace, persistentvolumeclaim, bucket_date)
			DO UPDATE SET
				persistentvolume = EXCLUDED.persistentvolume,
				storageclass = EXCLUDED.storageclass,
				capacity_bytes = GREATEST(daily_pvc_digests.capacity_bytes, EXCLUDED.capacity_bytes),
				request_bytes = GREATEST(daily_pvc_digests.request_bytes, EXCLUDED.request_bytes),
				usage_bytes_min = LEAST(daily_pvc_digests.usage_bytes_min, EXCLUDED.usage_bytes_min),
				usage_bytes_max = GREATEST(daily_pvc_digests.usage_bytes_max, EXCLUDED.usage_bytes_max),
				usage_bytes_avg = (daily_pvc_digests.usage_bytes_avg * daily_pvc_digests.sample_count + EXCLUDED.usage_bytes_avg * EXCLUDED.sample_count)
					/ NULLIF(daily_pvc_digests.sample_count + EXCLUDED.sample_count, 0),
				sample_count = daily_pvc_digests.sample_count + EXCLUDED.sample_count`,
			d.BucketDate, orgID, clusterUUID, d.Namespace,
			d.PVC, d.PV, d.StorageClass,
			d.CapacityBytes, d.RequestBytes,
			d.UsageBytesMin, d.UsageBytesMax, d.UsageBytesAvg,
			d.SampleCount,
		)
		if err != nil {
			return fmt.Errorf("upserting PVC digest %s/%s: %w", d.Namespace, d.PVC, err)
		}
	}
	return nil
}

// ProcessStorageCSV is the top-level entry point for storage CSV ingestion.
func ProcessStorageCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParsePVCRows(r)
	if err != nil {
		return fmt.Errorf("parsing storage CSV: %w", err)
	}
	if len(rows) == 0 {
		log.Infof("ProcessStorageCSV: no PVC rows found for cluster %s", clusterUUID)
		return nil
	}

	digests := ComputePVCDigests(rows)
	EnsurePVCDigestPartitions(ctx, pool, digests)
	if err := UpsertPVCDigests(ctx, pool, digests, orgID, clusterUUID); err != nil {
		return fmt.Errorf("upserting PVC digests: %w", err)
	}

	log.Infof("ProcessStorageCSV: upserted %d PVC digests for cluster %s", len(digests), clusterUUID)
	return nil
}
