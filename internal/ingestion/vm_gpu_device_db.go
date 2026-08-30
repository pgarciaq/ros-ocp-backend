package ingestion

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// UpsertVMGPUDevices replaces per-GPU rows for a digest (delete then insert).
func UpsertVMGPUDevices(ctx context.Context, pool *pgxpool.Pool, vmDigestID int64, devices []ingestGPUDeviceDigest) error {
	if len(devices) == 0 {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for GPU devices: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM vm_gpu_device_digests WHERE vm_digest_id = $1`, vmDigestID); err != nil {
		return fmt.Errorf("delete GPU devices: %w", err)
	}
	for _, dev := range devices {
		_, err := tx.Exec(ctx, `
			INSERT INTO vm_gpu_device_digests (
				vm_digest_id, gpu_uuid, gpu_model,
				util_avg_bp, util_max_bp,
				fb_used_avg_mib, fb_used_max_mib,
				sm_active_avg_bp, tensor_avg_bp, dram_avg_bp,
				mig_profile, max_slices
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			vmDigestID, dev.UUID, dev.Model,
			dev.UtilAvgBP, dev.UtilMaxBP,
			dev.FBUsedAvgMiB, dev.FBUsedMaxMiB,
			dev.SMActiveAvgBP, dev.TensorAvgBP, dev.DRAMAvgBP,
			dev.MIGProfile, dev.MaxSlices,
		)
		if err != nil {
			return fmt.Errorf("insert GPU device %s: %w", dev.UUID, err)
		}
	}
	return tx.Commit(ctx)
}

// IngestVMGPUDeviceCSV attaches per-device GPU metrics to existing daily VM
// digests. It streams CSV rows into all-hours and business-hours accumulators
// in one pass and does not retain a []VMGPUDeviceRow of every 15-minute sample.
func IngestVMGPUDeviceCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	allHoursAcc := make(map[vmGPUDeviceAccKey]*vmGPUDeviceAccumulator)
	var bhAcc map[vmGPUDeviceAccKey]*vmGPUDeviceAccumulator

	var cache *bhschedule.Cache
	if BusinessHoursAggregationEnabled() {
		var loadErr error
		cache, loadErr = bhschedule.LoadSchedules(ctx, pool, orgID, clusterUUID)
		if loadErr != nil {
			return fmt.Errorf("load business hours schedules for VM GPU devices: %w", loadErr)
		}
		if cache != nil && cache.ProducesBusinessHoursDigests() {
			bhAcc = make(map[vmGPUDeviceAccKey]*vmGPUDeviceAccumulator)
		}
	}

	n := 0
	_, err := forEachVMGPUDeviceCSVRow(ctx, r, func(row VMGPUDeviceRow) error {
		addVMGPUDeviceRow(allHoursAcc, row, nil)
		if bhAcc != nil {
			addVMGPUDeviceRow(bhAcc, row, VMGPUDeviceBusinessHoursWeight(cache))
		}
		n++
		return nil
	})
	if err != nil {
		return fmt.Errorf("parsing VM GPU device CSV: %w", err)
	}
	if n == 0 {
		return nil
	}

	digestMap := make(map[VMDigestKey]VMDigestResult)
	applyVMGPUDeviceAcc(allHoursAcc, digestMap)
	if err := upsertVMGPUDevicesForSchedule(ctx, pool, orgID, clusterUUID, digestMap, string(ScheduleTypeAllHours)); err != nil {
		return err
	}

	if bhAcc != nil {
		bhMap := make(map[VMDigestKey]VMDigestResult)
		applyVMGPUDeviceAcc(bhAcc, bhMap)
		if err := upsertVMGPUDevicesForSchedule(ctx, pool, orgID, clusterUUID, bhMap, string(ScheduleTypeBusinessHours)); err != nil {
			return err
		}
	}
	return nil
}

func upsertVMGPUDevicesForSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, digestMap map[VMDigestKey]VMDigestResult, scheduleType string) error {
	log := logging.ForOrg(orgID, clusterUUID)
	for key, d := range digestMap {
		if len(d.GPUDevices) == 0 {
			continue
		}
		digestID, err := LookupVMDigestID(ctx, pool, orgID, clusterUUID, key.VMName, key.Namespace, key.BucketDate.Format("2006-01-02"), scheduleType)
		if err != nil {
			if scheduleType == string(ScheduleTypeBusinessHours) {
				log.Warnf("VM GPU device CSV: no business_hours digest for %s/%s on %s, skipping BH devices",
					key.Namespace, key.VMName, key.BucketDate.Format("2006-01-02"))
			} else {
				log.Warnf("VM GPU device CSV: no digest for %s/%s on %s, skipping devices (ingest VM usage first)",
					key.Namespace, key.VMName, key.BucketDate.Format("2006-01-02"))
			}
			continue
		}
		if err := UpsertVMGPUDevices(ctx, pool, digestID, d.GPUDevices); err != nil {
			if scheduleType == string(ScheduleTypeBusinessHours) {
				return fmt.Errorf("upsert BH GPU devices for %s/%s: %w", key.Namespace, key.VMName, err)
			}
			return fmt.Errorf("upsert GPU devices for %s/%s: %w", key.Namespace, key.VMName, err)
		}
	}
	return nil
}

// LookupVMDigestID returns the digest primary key for a VM-day row of scheduleType
// (all_hours when empty).
func LookupVMDigestID(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, vmName, namespace, bucketDate, scheduleType string) (int64, error) {
	if scheduleType == "" {
		scheduleType = string(ScheduleTypeAllHours)
	}
	var id int64
	err := pool.QueryRow(ctx, `
		SELECT id FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = $3 AND namespace = $4
		  AND bucket_date = $5::date AND schedule_type = $6::digest_schedule_type`,
		orgID, clusterUUID, vmName, namespace, bucketDate, scheduleType,
	).Scan(&id)
	return id, err
}
