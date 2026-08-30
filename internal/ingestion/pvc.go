package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

const hourlyIntervalSeconds int64 = 3600

// PVCRow is a parsed storage CSV row (librobne/csv.PVCRow).
type PVCRow = libcsv.PVCRow

// forEachPVCRow parses storage CSV rows one at a time without retaining a full-slice copy.
func forEachPVCRow(ctx context.Context, r io.Reader, fn func(PVCRow) error) (int, error) {
	count := 0
	_, err := libcsv.ForEachPVC(ctx, r, func(row libcsv.PVCRow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

// ParsePVCRows parses the storage CSV into PVCRow structs.
// Processor ingest uses forEachPVCRow; this collector is for tests and callers
// that still want a slice. Empty PVC names are dropped. Bad timestamps are skipped.
func ParsePVCRows(r io.Reader) ([]PVCRow, error) {
	var rows []PVCRow
	_, err := forEachPVCRow(context.Background(), r, func(row PVCRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

// pvcDigestKey groups PVC rows by day + PVC identity.
type pvcDigestKey struct {
	Date      time.Time
	Namespace string
	PVC       string
}

type pvcDigestAcc struct {
	pv             string
	storageClass   string
	capacity       int64
	request        int64
	usageSum       int64
	usageMin       int64
	usageMax       int64
	count          int
	lastSeenPod    string
	vmName         string
	latestInterval time.Time
}

// PVCDigestResult is a daily aggregated PVC digest.
type PVCDigestResult struct {
	BucketDate    time.Time
	Namespace     string
	PVC           string
	LastSeenPod   string
	VMName        string
	PV            string
	StorageClass  string
	CapacityBytes int64
	RequestBytes  int64
	UsageBytesMin int64
	UsageBytesMax int64
	UsageBytesAvg int64
	SampleCount   int
}

func addPVCRowToDigests(groups map[pvcDigestKey]*pvcDigestAcc, r PVCRow) {
	date := r.IntervalStart.UTC().Truncate(24 * time.Hour)
	key := pvcDigestKey{Date: date, Namespace: r.Namespace, PVC: r.PersistentVolumeClaim}

	intervalSeconds := int64(r.IntervalEnd.Sub(r.IntervalStart).Seconds())
	if intervalSeconds <= 0 {
		intervalSeconds = hourlyIntervalSeconds
	}

	// Convert byte-seconds to bytes for capacity and usage (integer division with rounding).
	capacityBytes := r.CapacityBytes
	if r.UsageByteSeconds > 0 && capacityBytes > 1e12 {
		capacityBytes = (capacityBytes + intervalSeconds/2) / intervalSeconds
	}
	usageBytes := (r.UsageByteSeconds + hourlyIntervalSeconds/2) / hourlyIntervalSeconds
	requestBytes := (r.RequestByteSeconds + hourlyIntervalSeconds/2) / hourlyIntervalSeconds

	acc, ok := groups[key]
	if !ok {
		acc = &pvcDigestAcc{
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
	if r.Pod != "" && (acc.latestInterval.IsZero() || !r.IntervalEnd.Before(acc.latestInterval)) {
		acc.latestInterval = r.IntervalEnd
		acc.lastSeenPod = r.Pod
		if r.VMName != "" {
			acc.vmName = r.VMName
		}
	}
	if r.VMName != "" && acc.vmName == "" {
		acc.vmName = r.VMName
	}
}

func pvcDigestResults(groups map[pvcDigestKey]*pvcDigestAcc) []PVCDigestResult {
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
			LastSeenPod:   acc.lastSeenPod,
			VMName:        acc.vmName,
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

// ComputePVCDigests aggregates PVC rows into daily digests.
// The storage CSV uses byte-seconds; we convert to bytes by dividing
// by the interval duration (3600 seconds for hourly intervals).
func ComputePVCDigests(rows []PVCRow) []PVCDigestResult {
	groups := make(map[pvcDigestKey]*pvcDigestAcc)
	for _, r := range rows {
		addPVCRowToDigests(groups, r)
	}
	return pvcDigestResults(groups)
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
			logging.GetLogger().Warnf("EnsurePVCDigestPartitions: %s: %v (non-fatal)", partName, err)
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
				sample_count, last_seen_pod, vm_name
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (cluster_uuid, namespace, persistentvolumeclaim, bucket_date)
			DO UPDATE SET
				persistentvolume = EXCLUDED.persistentvolume,
				storageclass = EXCLUDED.storageclass,
				last_seen_pod = CASE
					WHEN EXCLUDED.last_seen_pod != '' THEN EXCLUDED.last_seen_pod
					ELSE daily_pvc_digests.last_seen_pod
				END,
				vm_name = CASE
					WHEN EXCLUDED.vm_name != '' THEN EXCLUDED.vm_name
					ELSE daily_pvc_digests.vm_name
				END,
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
			d.SampleCount, d.LastSeenPod, d.VMName,
		)
		if err != nil {
			return fmt.Errorf("upserting PVC digest %s/%s: %w", d.Namespace, d.PVC, err)
		}
	}
	return nil
}

// ProcessStorageCSV is the top-level entry point for storage CSV ingestion.
// It streams rows into digest accumulators and does not retain a []PVCRow of
// every hourly line.
func ProcessStorageCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	groups := make(map[pvcDigestKey]*pvcDigestAcc)
	n := 0
	_, err := forEachPVCRow(ctx, r, func(row PVCRow) error {
		addPVCRowToDigests(groups, row)
		n++
		return nil
	})
	if err != nil {
		return fmt.Errorf("parsing storage CSV: %w", err)
	}
	if n == 0 {
		logging.GetLogger().WithField("cluster_uuid", clusterUUID).Info("ProcessStorageCSV: no PVC rows found")
		return nil
	}

	digests := pvcDigestResults(groups)
	EnsurePVCDigestPartitions(ctx, pool, digests)
	if err := UpsertPVCDigests(ctx, pool, digests, orgID, clusterUUID); err != nil {
		return fmt.Errorf("upserting PVC digests: %w", err)
	}

	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof("ProcessStorageCSV: upserted %d PVC digests", len(digests))
	return nil
}
