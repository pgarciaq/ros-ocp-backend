package ingestion

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// UpsertVMPVCDigests replaces per-PVC rows for a digest (delete then insert).
func UpsertVMPVCDigests(ctx context.Context, pool *pgxpool.Pool, vmDigestID int64, pvcs []IngestPVCDigest) error {
	if len(pvcs) == 0 {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for PVC digests: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM vm_pvc_digests WHERE vm_digest_id = $1`, vmDigestID); err != nil {
		return fmt.Errorf("delete PVC digests: %w", err)
	}
	for _, pvc := range pvcs {
		_, err := tx.Exec(ctx, `
			INSERT INTO vm_pvc_digests (
				vm_digest_id, pvc_name, disk_capacity_bytes, volume_mode
			) VALUES ($1, $2, $3, $4)`,
			vmDigestID, pvc.PVCName, pvc.DiskCapacityBytes, pvc.VolumeMode,
		)
		if err != nil {
			return fmt.Errorf("insert PVC digest %s: %w", pvc.PVCName, err)
		}
	}
	return tx.Commit(ctx)
}

// IngestVMPVCCSV attaches per-PVC storage data to existing daily VM digests.
func IngestVMPVCCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseVMPVCCSVRows(ctx, r)
	if err != nil {
		return fmt.Errorf("parsing VM PVC CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	digestPVCs := MergeVMPVCRowsIntoDigests(rows)
	log := logging.ForOrg(orgID, clusterUUID)
	for key, pvcs := range digestPVCs {
		if len(pvcs) == 0 {
			continue
		}
		digestID, err := LookupVMDigestID(ctx, pool, orgID, clusterUUID, key.VMName, key.Namespace, key.BucketDate.Format("2006-01-02"))
		if err != nil {
			log.Warnf("VM PVC CSV: no digest for %s/%s on %s, skipping PVCs (ingest VM usage first)",
				key.Namespace, key.VMName, key.BucketDate.Format("2006-01-02"))
			continue
		}
		if err := UpsertVMPVCDigests(ctx, pool, digestID, pvcs); err != nil {
			return fmt.Errorf("upsert PVC digests for %s/%s: %w", key.Namespace, key.VMName, err)
		}
	}
	return nil
}
