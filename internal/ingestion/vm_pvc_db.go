package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// UpsertVMPVCDigests replaces per-PVC rows for a digest (delete then insert)
// using pgx.Batch to send all INSERTs in a single network round-trip.
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

	batch := &pgx.Batch{}
	for _, pvc := range pvcs {
		batch.Queue(`
			INSERT INTO vm_pvc_digests (
				vm_digest_id, pvc_name, disk_capacity_bytes, volume_mode
			) VALUES ($1, $2, $3, $4)`,
			vmDigestID, pvc.PVCName, pvc.DiskCapacityBytes, pvc.VolumeMode,
		)
	}
	br := tx.SendBatch(ctx, batch)
	for i := range pvcs {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return fmt.Errorf("insert PVC digest %s: %w", pvcs[i].PVCName, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("close PVC batch: %w", err)
	}
	return tx.Commit(ctx)
}

// BatchLookupVMDigestIDs fetches digest IDs for multiple (vm_name, namespace,
// bucket_date) keys in a single query, returning a map for O(1) lookup.
func BatchLookupVMDigestIDs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, keys []VMDigestKey) (map[VMDigestKey]int64, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	vmNames := make([]string, len(keys))
	namespaces := make([]string, len(keys))
	bucketDates := make([]string, len(keys))
	for i, k := range keys {
		vmNames[i] = k.VMName
		namespaces[i] = k.Namespace
		bucketDates[i] = k.BucketDate.Format("2006-01-02")
	}

	rows, err := pool.Query(ctx, `
		SELECT id, vm_name, namespace, bucket_date
		FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND (vm_name, namespace, bucket_date) IN (
		    SELECT unnest($3::text[]), unnest($4::text[]), unnest($5::date[])
		  )`,
		orgID, clusterUUID, vmNames, namespaces, bucketDates,
	)
	if err != nil {
		return nil, fmt.Errorf("batch lookup VM digest IDs: %w", err)
	}
	defer rows.Close()

	result := make(map[VMDigestKey]int64, len(keys))
	for rows.Next() {
		var id int64
		var vmName, namespace string
		var bucketDate time.Time
		if err := rows.Scan(&id, &vmName, &namespace, &bucketDate); err != nil {
			return nil, fmt.Errorf("scan VM digest ID: %w", err)
		}
		key := VMDigestKey{VMName: vmName, Namespace: namespace, BucketDate: bucketDate}
		result[key] = id
	}
	return result, rows.Err()
}

// IngestVMPVCCSV attaches per-PVC storage data to existing daily VM digests.
// Digest IDs are fetched in a single batch query, then PVC rows are upserted
// per VM using pgx.Batch within each transaction.
func IngestVMPVCCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseVMPVCCSVRows(ctx, r)
	if err != nil {
		return fmt.Errorf("parsing VM PVC CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	digestPVCs := MergeVMPVCRowsIntoDigests(rows)

	keys := make([]VMDigestKey, 0, len(digestPVCs))
	for k := range digestPVCs {
		keys = append(keys, k)
	}

	idMap, err := BatchLookupVMDigestIDs(ctx, pool, orgID, clusterUUID, keys)
	if err != nil {
		return fmt.Errorf("batch lookup digest IDs: %w", err)
	}

	log := logging.ForOrg(orgID, clusterUUID)
	for key, pvcs := range digestPVCs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(pvcs) == 0 {
			continue
		}
		digestID, ok := idMap[key]
		if !ok {
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
