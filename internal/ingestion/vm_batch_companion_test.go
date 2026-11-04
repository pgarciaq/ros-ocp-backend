package ingestion

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #757 (V15-07) regression suite: companions (PVC / GPU-device CSVs) must
// persist against the parents their usage creates IN THE SAME MANIFEST,
// regardless of file order. The baseline ingested companions immediately
// (querying persisted parents) while usage only accumulated until Flush —
// so for a genuinely new VM/day the companion skip was guaranteed, the file
// was still marked done, and same-manifest replay never repaired the loss
// (the audit's done=3 parent=1 pvc=0 gpu=0).
//
// Tier labeling (per the #756/#759 discipline):
//   - T1/T2/T4 are the behavioral REDs on the reviewed baseline: they use
//     the existing AddFile/Flush API and assert ATTACHED ROWS, which the
//     baseline cannot produce for a new VM/day.
//   - T3 (replay stability) is a replacement-semantics guard: green on the
//     fixed code by construction; on the baseline the first batch loses the
//     companions (T1's RED) and a second batch attaches them only because
//     the parent then exists — the done-marking replay gap is covered at
//     the services level (parallel_ingest_companion_test.go).

const companionTestDay = "2026-10-25"

// vmCompanionPVCCSV builds one PVC companion CSV for the vmSplitDayFile
// fixture VM (det-vm / vm-recs) on the same day.
func vmCompanionPVCCSV() string {
	return `interval_start,interval_end,vm_name,namespace,node_name,pvc_name,disk_capacity_bytes,volume_mode
` + companionTestDay + `T12:00:00Z,` + companionTestDay + `T12:15:00Z,det-vm,vm-recs,worker-1,data-pvc,107374182400,Filesystem
` + companionTestDay + `T12:00:00Z,` + companionTestDay + `T12:15:00Z,det-vm,vm-recs,worker-1,logs-pvc,53687091200,Filesystem
`
}

// vmCompanionGPUCSV builds one GPU-device companion CSV for the same VM/day.
func vmCompanionGPUCSV() string {
	return vmGPUDeviceCSVHeader() + `
` + companionTestDay + `T12:00:00Z,vm-recs,det-vm,gpu-aaa,A100,0.10,0.25,1024,2048,0.05,0.02,0.03,1g.5gb,7
` + companionTestDay + `T12:00:00Z,vm-recs,det-vm,gpu-bbb,H100,0.20,0.40,2048,4096,0.06,0.03,0.04,2g.10gb,4
`
}

// companionAttachmentCounts returns (pvcRows, gpuRows) attached to the
// fixture VM's digest for the companion test day.
func companionAttachmentCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (int, int) {
	t.Helper()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	var pvcRows int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM vm_pvc_digests p
		JOIN daily_vm_digests d ON p.vm_digest_id = d.id
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 AND d.vm_name = 'det-vm'
			AND d.namespace = 'vm-recs' AND d.bucket_date = $3 AND d.schedule_type = 'all_hours'`,
		orgID, cluster, companionTestDay).Scan(&pvcRows)
	require.NoError(t, err, "PVC attachment count query")
	var gpuRows int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM vm_gpu_device_digests g
		JOIN daily_vm_digests d ON g.vm_digest_id = d.id
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 AND d.vm_name = 'det-vm'
			AND d.namespace = 'vm-recs' AND d.bucket_date = $3 AND d.schedule_type = 'all_hours'`,
		orgID, cluster, companionTestDay).Scan(&gpuRows)
	require.NoError(t, err, "GPU attachment count query")
	return pvcRows, gpuRows
}

// T1 (baseline RED): usage FIRST, then companions — the audit's exact
// ordering. All three files are for a genuinely new VM/day; the parent and
// BOTH attachment sets must persist from one batch.
func TestVMBatch_UsageFirstCompanionsPersist(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	batch, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)

	kind, _, err := batch.AddFile(ctx, strings.NewReader(vmSplitDayFile(0, 96)))
	require.NoError(t, err)
	assert.Equal(t, "vm", kind)
	kind, _, err = batch.AddFile(ctx, strings.NewReader(vmCompanionPVCCSV()))
	require.NoError(t, err)
	assert.Equal(t, "vm-pvc", kind)
	kind, _, err = batch.AddFile(ctx, strings.NewReader(vmCompanionGPUCSV()))
	require.NoError(t, err)
	assert.Equal(t, "vm-gpu", kind)

	require.NoError(t, batch.Flush(ctx))

	pvcRows, gpuRows := companionAttachmentCounts(t, ctx, pool)
	assert.Equal(t, 2, pvcRows, "both PVC attachments must persist against the same-manifest parent (#757)")
	assert.Equal(t, 2, gpuRows, "both GPU device attachments must persist against the same-manifest parent (#757)")
}

// T2 (baseline RED): companions FIRST, usage last — order independence.
func TestVMBatch_CompanionFirstCompanionsPersist(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	batch, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)

	kind, _, err := batch.AddFile(ctx, strings.NewReader(vmCompanionPVCCSV()))
	require.NoError(t, err)
	assert.Equal(t, "vm-pvc", kind)
	kind, _, err = batch.AddFile(ctx, strings.NewReader(vmCompanionGPUCSV()))
	require.NoError(t, err)
	assert.Equal(t, "vm-gpu", kind)
	kind, _, err = batch.AddFile(ctx, strings.NewReader(vmSplitDayFile(0, 96)))
	require.NoError(t, err)
	assert.Equal(t, "vm", kind)

	require.NoError(t, batch.Flush(ctx))

	pvcRows, gpuRows := companionAttachmentCounts(t, ctx, pool)
	assert.Equal(t, 2, pvcRows, "PVC attachments must persist regardless of file order (#757)")
	assert.Equal(t, 2, gpuRows, "GPU attachments must persist regardless of file order (#757)")
}

// T3 (guard): replaying the same manifest's files as a second batch is
// idempotent — same parent row, same attachment sets (per-file
// delete-then-insert replacement), no duplication.
func TestVMBatch_ReplayIsIdempotent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	for i := 0; i < 2; i++ {
		batch, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
		require.NoError(t, err)
		_, _, err = batch.AddFile(ctx, strings.NewReader(vmSplitDayFile(0, 96)))
		require.NoError(t, err)
		_, _, err = batch.AddFile(ctx, strings.NewReader(vmCompanionPVCCSV()))
		require.NoError(t, err)
		_, _, err = batch.AddFile(ctx, strings.NewReader(vmCompanionGPUCSV()))
		require.NoError(t, err)
		require.NoError(t, batch.Flush(ctx))
	}

	var parents int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = 'det-vm'
			AND namespace = 'vm-recs' AND bucket_date = $3 AND schedule_type = 'all_hours'`,
		orgID, cluster, companionTestDay).Scan(&parents)
	require.NoError(t, err)
	assert.Equal(t, 1, parents, "one parent row after replay")

	pvcRows, gpuRows := companionAttachmentCounts(t, ctx, pool)
	assert.Equal(t, 2, pvcRows, "PVC set unchanged after replay (replacement, not accumulation)")
	assert.Equal(t, 2, gpuRows, "GPU set unchanged after replay (replacement, not accumulation)")
}

// F5 (fresh-context review finding): a lookup failure that is NOT a missing
// parent (cancelled context, DB error) must surface as an error, not be
// misreported as a missing-parent orphan.
func TestAttachVMGPUDevicesForSchedule_CancelledContextIsErrorNotMissing(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	digestMap := map[VMDigestKey]VMDigestResult{
		{VMName: "x", Namespace: "ns", BucketDate: time.Now().UTC()}: {
			GPUDevices: []ingestGPUDeviceDigest{{UUID: "u", Model: "M"}},
		},
	}
	missing, err := attachVMGPUDevicesForSchedule(ctx, pool, "1234567", testutil.TestClusterUUID, digestMap, string(ScheduleTypeAllHours))
	require.Error(t, err, "a cancelled context must surface as an error, not a missing-parent orphan (#757 review finding)")
	assert.Nil(t, missing, "no missing-parent report may accompany a real error")
}

// T5 (new-API guard): a companion whose usage parent never materializes is
// reported as orphaned with the missing parent named — NOT successful
// persistence — while attached companions and usage are not orphaned.
func TestVMBatch_OrphanedCompanionReported(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	orphanPVC := `interval_start,interval_end,vm_name,namespace,node_name,pvc_name,disk_capacity_bytes,volume_mode
` + companionTestDay + `T12:00:00Z,` + companionTestDay + `T12:15:00Z,ghost-vm,vm-recs,worker-1,ghost-pvc,107374182400,Filesystem
`
	orphanGPU := vmGPUDeviceCSVHeader() + `
` + companionTestDay + `T12:00:00Z,vm-recs,ghost-vm,gpu-ghost,H100,0.20,0.40,2048,4096,0.06,0.03,0.04,2g.10gb,4
`

	batch, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)

	// Usage for det-vm only: ghost-vm's usage never arrives anywhere.
	_, _, err = batch.AddFile(ctx, strings.NewReader(vmSplitDayFile(0, 96)))
	require.NoError(t, err)
	_, attachedPVC, err := batch.AddFile(ctx, strings.NewReader(vmCompanionPVCCSV()))
	require.NoError(t, err)
	require.NotNil(t, attachedPVC)
	_, attachedGPU, err := batch.AddFile(ctx, strings.NewReader(vmCompanionGPUCSV()))
	require.NoError(t, err)
	require.NotNil(t, attachedGPU)
	_, orphan1, err := batch.AddFile(ctx, strings.NewReader(orphanPVC))
	require.NoError(t, err)
	require.NotNil(t, orphan1)
	_, orphan2, err := batch.AddFile(ctx, strings.NewReader(orphanGPU))
	require.NoError(t, err)
	require.NotNil(t, orphan2)

	require.NoError(t, batch.Flush(ctx))

	assert.False(t, attachedPVC.Orphaned(), "the PVC companion whose parent exists is not orphaned")
	assert.False(t, attachedGPU.Orphaned(), "the GPU companion whose parent exists is not orphaned")
	assert.True(t, orphan1.Orphaned(), "the PVC companion for a never-seen VM must be reported orphaned (#757)")
	assert.Contains(t, orphan1.MissingParents(), "vm-recs/ghost-vm/"+companionTestDay,
		"the missing parent must be named")
	assert.True(t, orphan2.Orphaned(), "the GPU companion for a never-seen VM must be reported orphaned (#757)")

	// The attached rows still landed; the orphans' rows did not.
	pvcRows, gpuRows := companionAttachmentCounts(t, ctx, pool)
	assert.Equal(t, 2, pvcRows, "only the attached companion's rows persist")
	assert.Equal(t, 2, gpuRows, "only the attached companion's rows persist")
}

// T6 (guard, close criterion "failures between parent persistence and
// attachment"): a failure at the companion flush seam — after parents are
// written, before attachments apply — fails the Flush, leaves attachments
// absent, and (at the services layer) marks nothing done so the idempotent
// manifest retry re-processes everything.
func TestVMBatch_FailureBetweenParentAndAttachmentFailsFlush(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	origHook := companionFlushHook
	companionFlushHook = func() error { return fmt.Errorf("injected failure at the companion seam") }
	t.Cleanup(func() { companionFlushHook = origHook })

	batch, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	_, _, err = batch.AddFile(ctx, strings.NewReader(vmSplitDayFile(0, 96)))
	require.NoError(t, err)
	_, _, err = batch.AddFile(ctx, strings.NewReader(vmCompanionPVCCSV()))
	require.NoError(t, err)

	flushErr := batch.Flush(ctx)
	require.Error(t, flushErr, "a failure between parent persistence and attachment must fail the flush")

	// The parent landed (Flush writes it first); the attachment did not.
	var parents int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = 'det-vm'
			AND namespace = 'vm-recs' AND bucket_date = $3 AND schedule_type = 'all_hours'`,
		orgID, cluster, companionTestDay).Scan(&parents))
	assert.Equal(t, 1, parents, "the parent was persisted before the seam")
	pvcRows, _ := companionAttachmentCounts(t, ctx, pool)
	assert.Equal(t, 0, pvcRows, "the attachment did not land past the seam")
}

// T4 (baseline RED): a later payload replacing the usage must keep the
// first payload's attachments valid — the digest row's identity survives
// ON CONFLICT DO UPDATE, so per-attachment fidelity outlives replacement.
func TestVMBatch_LaterPayloadReplacesUsageKeepsAttachments(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	b1, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	_, _, err = b1.AddFile(ctx, strings.NewReader(vmSplitDayFile(0, 96)))
	require.NoError(t, err)
	_, _, err = b1.AddFile(ctx, strings.NewReader(vmCompanionPVCCSV()))
	require.NoError(t, err)
	_, _, err = b1.AddFile(ctx, strings.NewReader(vmCompanionGPUCSV()))
	require.NoError(t, err)
	require.NoError(t, b1.Flush(ctx))

	// The later payload re-sends half the day with different usage and no
	// companions (KMO's monthly-accumulator shape).
	b2, err := NewVMDigestBatch(ctx, pool, orgID, cluster)
	require.NoError(t, err)
	_, _, err = b2.AddFile(ctx, strings.NewReader(vmSplitDayFile(48, 96)))
	require.NoError(t, err)
	require.NoError(t, b2.Flush(ctx))

	var sampleCount int
	err = pool.QueryRow(ctx, `
		SELECT sample_count FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND vm_name = 'det-vm'
			AND namespace = 'vm-recs' AND bucket_date = $3 AND schedule_type = 'all_hours'`,
		orgID, cluster, companionTestDay).Scan(&sampleCount)
	require.NoError(t, err)
	assert.Equal(t, 48, sampleCount, "the later payload replaces usage (existing #707 semantics)")

	pvcRows, gpuRows := companionAttachmentCounts(t, ctx, pool)
	assert.Equal(t, 2, pvcRows, "PVC attachments survive usage replacement (#757)")
	assert.Equal(t, 2, gpuRows, "GPU attachments survive usage replacement (#757)")
}
