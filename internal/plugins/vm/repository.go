package vm

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// UpsertDailyVMDigests persists VM daily digests (INSERT ... ON CONFLICT DO UPDATE).
func UpsertDailyVMDigests(ctx context.Context, pool *pgxpool.Pool, digests []model.DailyVMDigest) error {
	if len(digests) == 0 {
		return nil
	}

	orgID := digests[0].OrgID
	clusterUUID := digests[0].ClusterUUID.String()
	rows := make([]ingestion.VMDigestResult, len(digests))
	for i, d := range digests {
		rows[i] = ingestion.VMDigestResult{
			VMName:                  d.VMName,
			Namespace:               d.Namespace,
			NodeName:                d.NodeName,
			GuestOS:                 d.GuestOS,
			BucketDate:              d.BucketDate,
			CPUUsageP50MC:           d.CPUUsageP50MC,
			CPUUsageP95MC:           d.CPUUsageP95MC,
			CPUUsageP99MC:           d.CPUUsageP99MC,
			CPUUsageMaxMC:           d.CPUUsageMaxMC,
			CPURequestMC:            d.CPURequestMC,
			CPULimitMC:              d.CPULimitMC,
			MemUsageP50KiB:          d.MemUsageP50KiB,
			MemUsageP95KiB:          d.MemUsageP95KiB,
			MemUsageP99KiB:          d.MemUsageP99KiB,
			MemUsageMaxKiB:          d.MemUsageMaxKiB,
			MemRequestKiB:           d.MemRequestKiB,
			MemAvailableP50KiB:      d.MemAvailableP50KiB,
			MemAvailableP95KiB:      d.MemAvailableP95KiB,
			DiskAllocatedMaxBytes:   d.DiskAllocatedMaxBytes,
			FilesystemUsedMaxBytes:  d.FilesystemUsedMaxBytes,
			FilesystemCapacityBytes: d.FilesystemCapacityBytes,
			DiskReadIOPSP95:         d.DiskReadIOPSP95,
			DiskWriteIOPSP95:        d.DiskWriteIOPSP95,
			DiskReadBPS95:           d.DiskReadBPS95,
			DiskWriteBPS95:          d.DiskWriteBPS95,
			SampleCount:             d.SampleCount,
			AgentSampleCount:        d.AgentSampleCount,
		}
	}
	return ingestion.UpsertDailyVMDigests(ctx, pool, orgID, clusterUUID, rows)
}

// upsertDigestResults persists ingestion-layer digest rows for a cluster.
func upsertDigestResults(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, digests []ingestion.VMDigestResult) error {
	return ingestion.UpsertDailyVMDigests(ctx, pool, orgID, clusterUUID, digests)
}

// GetDailyVMDigests returns VM daily digests for a cluster since the given date.
func GetDailyVMDigests(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID, since time.Time) ([]model.DailyVMDigest, error) {
	return engine.QueryDailyVMDigests(ctx, pool, orgID, clusterUUID, since)
}

// UpsertVMRecommendations persists VM recommendations (INSERT ... ON CONFLICT DO UPDATE).
func UpsertVMRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []model.VMRecommendation) error {
	return engine.PersistVMRecommendations(ctx, pool, recs, nil)
}
