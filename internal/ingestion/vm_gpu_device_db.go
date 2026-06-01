package ingestion

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

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

// IngestVMGPUDeviceCSV attaches per-device GPU metrics to existing daily VM digests.
func IngestVMGPUDeviceCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseVMGPUDeviceCSVRows(r)
	if err != nil {
		return fmt.Errorf("parsing VM GPU device CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	digestMap := make(map[VMDigestKey]VMDigestResult)
	MergeVMGPUDeviceRowsIntoDigests(digestMap, rows)

	log := logging.ForOrg(orgID, clusterUUID)
	for key, d := range digestMap {
		if len(d.GPUDevices) == 0 {
			continue
		}
		digestID, err := LookupVMDigestID(ctx, pool, orgID, clusterUUID, key.VMName, key.Namespace, key.BucketDate.Format("2006-01-02"))
		if err != nil {
			log.Warnf("VM GPU device CSV: no digest for %s/%s on %s, skipping devices (ingest VM usage first)",
				key.Namespace, key.VMName, key.BucketDate.Format("2006-01-02"))
			continue
		}
		if err := UpsertVMGPUDevices(ctx, pool, digestID, d.GPUDevices); err != nil {
			return fmt.Errorf("upsert GPU devices for %s/%s: %w", key.Namespace, key.VMName, err)
		}
	}
	return nil
}

// LookupVMDigestID returns the digest primary key for a VM-day row.
func LookupVMDigestID(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, vmName, namespace string, bucketDate string) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		SELECT id FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = $3 AND namespace = $4 AND bucket_date = $5::date`,
		orgID, clusterUUID, vmName, namespace, bucketDate,
	).Scan(&id)
	return id, err
}
