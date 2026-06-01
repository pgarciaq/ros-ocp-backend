package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// AttachGPUDevicesToDigests loads vm_gpu_device_digests rows for the given digests.
func AttachGPUDevicesToDigests(ctx context.Context, pool *pgxpool.Pool, digests []model.DailyVMDigest) error {
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
		SELECT vm_digest_id, gpu_uuid, gpu_model,
			util_avg_bp, util_max_bp,
			fb_used_avg_mib, fb_used_max_mib,
			sm_active_avg_bp, tensor_avg_bp, dram_avg_bp,
			mig_profile, max_slices
		FROM vm_gpu_device_digests
		WHERE vm_digest_id = ANY($1)
		ORDER BY vm_digest_id, gpu_uuid`, ids)
	if err != nil {
		return fmt.Errorf("query VM GPU devices: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var digestID int64
		var dev model.GPUDeviceDigest
		if err := rows.Scan(
			&digestID, &dev.UUID, &dev.Model,
			&dev.UtilAvgBP, &dev.UtilMaxBP,
			&dev.FBUsedAvgMiB, &dev.FBUsedMaxMiB,
			&dev.SMActiveAvgBP, &dev.TensorAvgBP, &dev.DRAMAvgBP,
			&dev.MIGProfile, &dev.MaxSlices,
		); err != nil {
			return fmt.Errorf("scan VM GPU device: %w", err)
		}
		for _, idx := range idToIdx[digestID] {
			digests[idx].Devices = append(digests[idx].Devices, dev)
		}
	}
	return rows.Err()
}
