package ingestion

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// Regression test: hourly digest upserts must accept int64-scale usage totals.
// A node-hour summing past 2^31 KiB (observed: 5129184234) aborted the pgx
// batch, retried the Kafka message 5x, and routed the whole manifest to DLQ.
// Sibling daily digest tables are BIGINT; the hourly tables must match.
func TestUpsertHourlyDigests_AcceptsBeyondInt32(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	day := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	const clusterUUID = "550e8400-e29b-41d4-a716-446655440001"
	const bigMemKiB = int64(5129184234) // > math.MaxInt32, from production-shape data

	nodeDigests := map[NodeHourlyDigestKey]NodeHourlyDigestResult{
		{NodeName: "overflow-node", BucketDate: day, Hour: 3}: {
			NodeName:       "overflow-node",
			BucketDate:     day,
			Hour:           3,
			CPUUsageP95MC:  8000,
			MemUsageP95KiB: bigMemKiB,
			SampleCount:    1,
			MaxPodCount:    4,
		},
	}
	require.NoError(t, UpsertHourlyNodeDigests(ctx, pool, "overflow-org", clusterUUID, nodeDigests))

	var gotMem int64
	err := pool.QueryRow(ctx, `SELECT mem_usage_p95_kib FROM hourly_node_digests
		WHERE org_id='overflow-org' AND node_name='overflow-node'`).Scan(&gotMem)
	require.NoError(t, err)
	require.Equal(t, bigMemKiB, gotMem)

	vmDigests := map[VMHourlyDigestKey]VMHourlyDigestResult{
		{VMName: "overflow-vm", Namespace: "ns", BucketDate: day, Hour: 5}: {
			VMName:           "overflow-vm",
			Namespace:        "ns",
			BucketDate:       day,
			Hour:             5,
			CPUUsageP95MC:    4000,
			MemUsageP95KiB:   bigMemKiB,
			SampleCount:      1,
			DiskReadIOPSP95:  100,
			DiskWriteIOPSP95: 200,
		},
	}
	require.NoError(t, UpsertHourlyVMDigests(ctx, pool, "overflow-org", clusterUUID, vmDigests))

	err = pool.QueryRow(ctx, `SELECT mem_usage_p95_kib FROM hourly_vm_digests
		WHERE org_id='overflow-org' AND vm_name='overflow-vm'`).Scan(&gotMem)
	require.NoError(t, err)
	require.Equal(t, bigMemKiB, gotMem)
}
