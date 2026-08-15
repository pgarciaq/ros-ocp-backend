package vm

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AttachPVCsToDigests loads vm_pvc_digests rows for the given digests.
func AttachPVCsToDigests(ctx context.Context, pool *pgxpool.Pool, digests []Digest) error {
	if len(digests) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(digests))
	idToIdx := make(map[int64][]int, len(digests))
	for i, d := range digests {
		if d.ID == 0 {
			continue
		}
		ids = append(ids, d.ID)
		idToIdx[d.ID] = append(idToIdx[d.ID], i)
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := pool.Query(ctx, `
		SELECT vm_digest_id, pvc_name, disk_capacity_bytes, volume_mode
		FROM vm_pvc_digests
		WHERE vm_digest_id = ANY($1)
		ORDER BY vm_digest_id, pvc_name`, ids)
	if err != nil {
		return fmt.Errorf("query VM PVC digests: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var digestID int64
		var pvc PVCDigest
		if err := rows.Scan(
			&digestID, &pvc.PVCName, &pvc.DiskCapacityBytes, &pvc.VolumeMode,
		); err != nil {
			return fmt.Errorf("scan VM PVC digest: %w", err)
		}
		for _, idx := range idToIdx[digestID] {
			digests[idx].PVCs = append(digests[idx].PVCs, pvc)
		}
	}
	return rows.Err()
}
