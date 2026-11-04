package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// #757 (V15-07), services level: processVMFileBatch must not mark companion
// files done unless their attachments actually persisted. The baseline marked
// companions done after a successful flush even though their rows never landed
// (the skip was logged as success) — and same-manifest replay then skipped the
// done files, so the loss was permanent. This is the done-marking RED the
// ingestion-level suite cannot express.

func TestProcessVMFileBatch_CompanionsPersistBeforeDone(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-vm-companion"
	clusterUUID := "bbbbbbbb-1111-2222-3333-444444444444"

	// One manifest: usage + PVC + GPU companion for a genuinely new VM/day.
	usageCSV := vmCompanionTestUsageCSV()
	pvcCSV := vmCompanionTestPVCCSV()
	gpuCSV := vmCompanionTestGPUCSV()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		switch r.URL.Path {
		case "/ocp_ros_vm_usage_1.csv":
			fmt.Fprint(w, usageCSV)
		case "/ocp_ros_vm_pvc_1.csv":
			fmt.Fprint(w, pvcCSV)
		case "/ocp_ros_vm_gpu_device_1.csv":
			fmt.Fprint(w, gpuCSV)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-vm-companion",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_vm_usage_1.csv",
			ts.URL + "/ocp_ros_vm_pvc_1.csv",
			ts.URL + "/ocp_ros_vm_gpu_device_1.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-vm-companion"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "vm-companion-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-vm-companion-1"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)
	_ = rhAccount
	_ = cluster

	manifestID := kafkaMsg.Metadata.Manifest_id
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, manifestID,
		clusterUUID, orgID, kafkaMsg.Files, reportTypeForFilename))

	vmFiles := []vmFamilyFile{
		{idx: 0, file: kafkaMsg.Files[0]},
		{idx: 1, file: kafkaMsg.Files[1]},
		{idx: 2, file: kafkaMsg.Files[2]},
	}

	transientErr, permFailed := processVMFileBatch(ctx, pool, logging_entry(), kafkaMsg, manifestID, vmFiles)
	require.NoError(t, transientErr, "no transient error expected for a well-formed manifest")
	require.False(t, permFailed, "no permanent failure expected")

	// Every file must be marked done — with rows that actually landed.
	for _, filename := range []string{"ocp_ros_vm_usage_1.csv", "ocp_ros_vm_pvc_1.csv", "ocp_ros_vm_gpu_device_1.csv"} {
		status, err := model.GetReportFileStatus(ctx, pool, manifestID, filename)
		require.NoError(t, err, "file status readable for %s", filename)
		assert.Equal(t, model.ReportFileDone, status, "%s must be done", filename)
	}

	// THE #757 ASSERTION: done companions must have persisted attachments.
	// Baseline: done=3 but pvc=0 gpu=0 (done without persistence).
	var pvcRows, gpuRows int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM vm_pvc_digests p
		JOIN daily_vm_digests d ON p.vm_digest_id = d.id
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 AND d.vm_name = 'det-vm' AND d.bucket_date = '2026-10-25'`,
		orgID, clusterUUID).Scan(&pvcRows))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM vm_gpu_device_digests g
		JOIN daily_vm_digests d ON g.vm_digest_id = d.id
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 AND d.vm_name = 'det-vm' AND d.bucket_date = '2026-10-25'`,
		orgID, clusterUUID).Scan(&gpuRows))
	assert.Equal(t, 2, pvcRows, "PVC companion marked done must have persisted its rows (#757)")
	assert.Equal(t, 2, gpuRows, "GPU companion marked done must have persisted its rows (#757)")

	// Same-manifest replay: everything done → skipped → still consistent.
	transientErr, permFailed = processVMFileBatch(ctx, pool, logging_entry(), kafkaMsg, manifestID, vmFiles)
	assert.NoError(t, transientErr, "replay of a fully-done manifest is a clean no-op")
	assert.False(t, permFailed, "replay of a fully-done manifest is not a permanent failure")
}

// T7b: the orphan lifecycle at the services layer. A companion whose usage
// parent never arrives is NOT marked done and drives a transient error
// (the manifest retry ladder); when the usage finally lands (a later
// payload), the retry attaches the companion and completes the manifest.
func TestProcessVMFileBatch_OrphanedCompanionNotDoneUntilUsageArrives(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-vm-orphan"
	clusterUUID := "cccccccc-1111-2222-3333-444444444444"

	usageCSV := vmCompanionTestUsageCSV() // det-vm
	pvcCSV := vmCompanionTestPVCCSV()     // det-vm
	ghostGPUCSV := ingestion.CanonicalVMGPUDeviceCSVHeader() + `
2026-10-25T12:00:00Z,vm-recs,ghost-vm,gpu-ghost,H100,0.20,0.40,2048,4096,0.06,0.03,0.04,2g.10gb,4
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		switch r.URL.Path {
		case "/ocp_ros_vm_usage_1.csv":
			fmt.Fprint(w, usageCSV)
		case "/ocp_ros_vm_pvc_1.csv":
			fmt.Fprint(w, pvcCSV)
		case "/ocp_ros_vm_gpu_device_1.csv":
			fmt.Fprint(w, ghostGPUCSV)
		case "/ocp_ros_vm_usage_late.csv": // ghost-vm usage, served only once "uploaded"
			fmt.Fprint(w, ghostUsageCSV())
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-vm-orphan",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_vm_usage_1.csv",
			ts.URL + "/ocp_ros_vm_pvc_1.csv",
			ts.URL + "/ocp_ros_vm_gpu_device_1.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-vm-orphan"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "vm-orphan-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-vm-orphan-1"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)
	_ = rhAccount
	_ = cluster

	manifestID := kafkaMsg.Metadata.Manifest_id
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, manifestID,
		clusterUUID, orgID, kafkaMsg.Files, reportTypeForFilename))

	vmFiles := []vmFamilyFile{
		{idx: 0, file: kafkaMsg.Files[0]},
		{idx: 1, file: kafkaMsg.Files[1]},
		{idx: 2, file: kafkaMsg.Files[2]},
	}

	// Attempt 1: the ghost companion orphans — transient error, and the
	// dependent file must NOT be marked done while its batch-mates are.
	transientErr, permFailed := processVMFileBatch(ctx, pool, logging_entry(), kafkaMsg, manifestID, vmFiles)
	require.Error(t, transientErr, "the orphaned companion must drive a transient error (#757)")
	require.ErrorIs(t, transientErr, ingestion.ErrVMCompanionOrphaned,
		"the transient error must be the typed orphan error so the retry ladder classifies it")
	assert.False(t, permFailed, "an orphaned companion is not a permanent failure")

	status, err := model.GetReportFileStatus(ctx, pool, manifestID, "ocp_ros_vm_usage_1.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileDone, status, "the usage file is done (its data landed)")
	status, err = model.GetReportFileStatus(ctx, pool, manifestID, "ocp_ros_vm_pvc_1.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileDone, status, "the attached PVC companion is done")
	status, err = model.GetReportFileStatus(ctx, pool, manifestID, "ocp_ros_vm_gpu_device_1.csv")
	require.NoError(t, err)
	assert.NotEqual(t, model.ReportFileDone, status,
		"the orphaned GPU companion must NOT be done — a skipped parent dependency is not successful persistence (#757)")

	var gpuRows int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM vm_gpu_device_digests g
		JOIN daily_vm_digests d ON g.vm_digest_id = d.id
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 AND d.vm_name = 'ghost-vm'`,
		orgID, clusterUUID).Scan(&gpuRows))
	assert.Equal(t, 0, gpuRows, "the orphaned companion's rows must not have landed")

	// Attempt 2 (the ladder's retry, usage still absent): same outcome —
	// done files skip, the orphan re-processes and stays undone.
	transientErr, permFailed = processVMFileBatch(ctx, pool, logging_entry(), kafkaMsg, manifestID, vmFiles)
	require.ErrorIs(t, transientErr, ingestion.ErrVMCompanionOrphaned, "retry without usage: still orphaned")
	assert.False(t, permFailed)

	// The usage finally arrives — a later payload manifest for ghost-vm.
	lateMsg := types.KafkaMsg{
		Request_id:   "test-vm-orphan-late",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL + "/ocp_ros_vm_usage_late.csv"},
	}
	lateMsg.Metadata = kafkaMsg.Metadata
	lateMsg.Metadata.Manifest_id = "manifest-vm-orphan-late"
	lateFiles := []vmFamilyFile{{idx: 0, file: lateMsg.Files[0]}}
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, lateMsg.Metadata.Manifest_id,
		clusterUUID, orgID, lateMsg.Files, reportTypeForFilename))
	transientErr, permFailed = processVMFileBatch(ctx, pool, logging_entry(), lateMsg, lateMsg.Metadata.Manifest_id, lateFiles)
	require.NoError(t, transientErr, "the late usage payload itself is clean")
	require.False(t, permFailed)

	// Attempt 3 (the ladder re-drives the original manifest): the ghost
	// companion now attaches and the manifest completes.
	transientErr, permFailed = processVMFileBatch(ctx, pool, logging_entry(), kafkaMsg, manifestID, vmFiles)
	assert.NoError(t, transientErr, "once the usage parent exists, the retry attaches the companion (#757)")
	assert.False(t, permFailed)

	status, err = model.GetReportFileStatus(ctx, pool, manifestID, "ocp_ros_vm_gpu_device_1.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileDone, status, "the formerly-orphaned companion is done after recovery")

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM vm_gpu_device_digests g
		JOIN daily_vm_digests d ON g.vm_digest_id = d.id
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 AND d.vm_name = 'ghost-vm'`,
		orgID, clusterUUID).Scan(&gpuRows))
	assert.Equal(t, 1, gpuRows, "the recovered companion's rows landed")
}

// ghostUsageCSV is one day of usage for the never-seen ghost-vm, arriving in
// a later payload.
func ghostUsageCSV() string {
	var b strings.Builder
	b.WriteString(ingestion.CanonicalVMUsageCSVHeader())
	b.WriteByte('\n')
	for i := 0; i < 96; i++ {
		start := fmt.Sprintf("2026-10-25T%02d:%02d:00Z", i/4, (i%4)*15)
		end := "2026-10-26T00:00:00Z"
		if i+1 < 96 {
			end = fmt.Sprintf("2026-10-25T%02d:%02d:00Z", (i+1)/4, ((i+1)%4)*15)
		}
		fmt.Fprintf(&b, "%s,%s,ghost-vm,vm-recs,worker-2,linux,1500,2000,4000,1048576,4194304,1572864,107374182400,53687091200,107374182400,120,80,1048576,524288\n",
			start, end)
	}
	return b.String()
}

// F1 (fresh-context review finding): a VM-family transient error (the
// orphaned companion) must not block the manifest's other file families —
// the container file processes and is done on the SAME attempt, while the
// orphan transient still returns to drive the retry ladder.
func TestParallelIngestFiles_OrphanDoesNotBlockOtherFamilies(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-vm-mixed"
	clusterUUID := "dddddddd-1111-2222-3333-444444444444"

	ghostGPUCSV := ingestion.CanonicalVMGPUDeviceCSVHeader() + `
2026-10-25T12:00:00Z,vm-recs,ghost-vm,gpu-ghost,H100,0.20,0.40,2048,4096,0.06,0.03,0.04,2g.10gb,4
`
	containerCSV := buildTestCSV(1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		switch r.URL.Path {
		case "/ocp_ros_vm_usage_1.csv":
			fmt.Fprint(w, vmCompanionTestUsageCSV())
		case "/ocp_ros_vm_gpu_device_1.csv":
			fmt.Fprint(w, ghostGPUCSV)
		case "/ocp_ros_usage_1.csv":
			fmt.Fprint(w, containerCSV)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-vm-mixed",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_vm_usage_1.csv",
			ts.URL + "/ocp_ros_vm_gpu_device_1.csv",
			ts.URL + "/ocp_ros_usage_1.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-vm-mixed"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "vm-mixed-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-vm-mixed-1"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	manifestID := kafkaMsg.Metadata.Manifest_id
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, manifestID,
		clusterUUID, orgID, kafkaMsg.Files, reportTypeForFilename))

	transientErr, _ := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, manifestID, true, &rhAccount, &cluster)
	require.ErrorIs(t, transientErr, ingestion.ErrVMCompanionOrphaned,
		"the orphan transient must still return to drive the retry ladder")

	status, err := model.GetReportFileStatus(ctx, pool, manifestID, "ocp_ros_usage_1.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileDone, status,
		"the container file must process on the same attempt despite the VM orphan (#757 review finding)")

	status, err = model.GetReportFileStatus(ctx, pool, manifestID, "ocp_ros_vm_usage_1.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileDone, status, "the VM usage file is done")
}

// F2 (fresh-context review finding): when an orphaned earlier companion
// re-processes after a later companion already replaced the same VM's
// devices, the full companion set re-applies in file order — last file
// still wins. gpuA (earlier, partially orphaned) carries A100 for det-vm;
// gpuB (later) replaces with H200; after the orphan recovers, det-vm's
// device must be gpuB's H200, not gpuA's A100.
func TestProcessVMFileBatch_CompanionReplacementOrderSurvivesOrphanRetry(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-vm-order"
	clusterUUID := "eeeeeeee-1111-2222-3333-444444444444"

	gpuA := ingestion.CanonicalVMGPUDeviceCSVHeader() + `
2026-10-25T12:00:00Z,vm-recs,det-vm,gpu-aaa,A100,0.10,0.25,1024,2048,0.05,0.02,0.03,1g.5gb,7
2026-10-25T12:00:00Z,vm-recs,ghost-vm,gpu-ghost,H100,0.20,0.40,2048,4096,0.06,0.03,0.04,2g.10gb,4
`
	gpuB := ingestion.CanonicalVMGPUDeviceCSVHeader() + `
2026-10-25T13:00:00Z,vm-recs,det-vm,gpu-aaa,H200,0.30,0.50,3072,6144,0.07,0.04,0.05,3g.20gb,2
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		switch r.URL.Path {
		case "/ocp_ros_vm_usage_1.csv":
			fmt.Fprint(w, vmCompanionTestUsageCSV())
		case "/ocp_ros_vm_gpu_device_a.csv":
			fmt.Fprint(w, gpuA)
		case "/ocp_ros_vm_gpu_device_b.csv":
			fmt.Fprint(w, gpuB)
		case "/ocp_ros_vm_usage_late.csv":
			fmt.Fprint(w, ghostUsageCSV())
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-vm-order",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_vm_usage_1.csv",
			ts.URL + "/ocp_ros_vm_gpu_device_a.csv",
			ts.URL + "/ocp_ros_vm_gpu_device_b.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-vm-order"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "vm-order-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-vm-order-1"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)
	_ = rhAccount
	_ = cluster

	manifestID := kafkaMsg.Metadata.Manifest_id
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, manifestID,
		clusterUUID, orgID, kafkaMsg.Files, reportTypeForFilename))

	vmFiles := []vmFamilyFile{
		{idx: 0, file: kafkaMsg.Files[0]},
		{idx: 1, file: kafkaMsg.Files[1]},
		{idx: 2, file: kafkaMsg.Files[2]},
	}

	// Attempt 1: gpuA orphaned on ghost-vm; gpuB done with det-vm's H200.
	tErr, _ := processVMFileBatch(ctx, pool, logging_entry(), kafkaMsg, manifestID, vmFiles)
	require.ErrorIs(t, tErr, ingestion.ErrVMCompanionOrphaned)

	// The usage for ghost-vm arrives in a later payload.
	lateMsg := types.KafkaMsg{Request_id: "test-vm-order-late", B64_identity: "dGVzdA==",
		Files: []string{ts.URL + "/ocp_ros_vm_usage_late.csv"}}
	lateMsg.Metadata = kafkaMsg.Metadata
	lateMsg.Metadata.Manifest_id = "manifest-vm-order-late"
	lateFiles := []vmFamilyFile{{idx: 0, file: lateMsg.Files[0]}}
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, lateMsg.Metadata.Manifest_id,
		clusterUUID, orgID, lateMsg.Files, reportTypeForFilename))
	tErr, _ = processVMFileBatch(ctx, pool, logging_entry(), lateMsg, lateMsg.Metadata.Manifest_id, lateFiles)
	require.NoError(t, tErr)

	// Attempt 2: the orphan recovers; the full companion set re-applies in
	// file order, so gpuB's replacement (H200) must still win for det-vm.
	tErr, _ = processVMFileBatch(ctx, pool, logging_entry(), kafkaMsg, manifestID, vmFiles)
	require.NoError(t, tErr)

	var model_ string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT g.gpu_model FROM vm_gpu_device_digests g
		JOIN daily_vm_digests d ON g.vm_digest_id = d.id
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 AND d.vm_name = 'det-vm' AND g.gpu_uuid = 'gpu-aaa'`,
		orgID, clusterUUID).Scan(&model_))
	assert.Equal(t, "H200", model_,
		"after orphan recovery the later companion file's replacement must win (#757 review finding)")
}

// F3 (fresh-context review finding): autocommit mode has no retry ladder —
// the handler tail drops transient errors — so an orphaned companion is
// marked terminally failed (visible, not successful persistence) instead of
// being stranded in processing forever.
func TestProcessVMFileBatch_OrphanTerminalFailureUnderAutoCommit(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })
	t.Setenv("KAFKA_AUTO_COMMIT", "true")
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-vm-ac"
	clusterUUID := "ffffffff-1111-2222-3333-444444444444"

	ghostGPUCSV := ingestion.CanonicalVMGPUDeviceCSVHeader() + `
2026-10-25T12:00:00Z,vm-recs,ghost-vm,gpu-ghost,H100,0.20,0.40,2048,4096,0.06,0.03,0.04,2g.10gb,4
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		switch r.URL.Path {
		case "/ocp_ros_vm_usage_1.csv":
			fmt.Fprint(w, vmCompanionTestUsageCSV())
		case "/ocp_ros_vm_gpu_device_1.csv":
			fmt.Fprint(w, ghostGPUCSV)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-vm-ac",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_vm_usage_1.csv",
			ts.URL + "/ocp_ros_vm_gpu_device_1.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-vm-ac"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "vm-ac-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-vm-ac-1"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)
	_ = rhAccount
	_ = cluster

	manifestID := kafkaMsg.Metadata.Manifest_id
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, manifestID,
		clusterUUID, orgID, kafkaMsg.Files, reportTypeForFilename))

	vmFiles := []vmFamilyFile{
		{idx: 0, file: kafkaMsg.Files[0]},
		{idx: 1, file: kafkaMsg.Files[1]},
	}

	transientErr, permFailed := processVMFileBatch(ctx, pool, logging_entry(), kafkaMsg, manifestID, vmFiles)
	assert.NoError(t, transientErr, "autocommit mode: the orphan is terminal, not transient")
	assert.True(t, permFailed, "autocommit mode: the orphaned companion counts as a file failure")

	status, err := model.GetReportFileStatus(ctx, pool, manifestID, "ocp_ros_vm_gpu_device_1.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileFailed, status,
		"autocommit mode: the orphaned companion must be terminally failed, not stranded in processing (#757 review finding)")

	status, err = model.GetReportFileStatus(ctx, pool, manifestID, "ocp_ros_vm_usage_1.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileDone, status, "the usage file is done")
}

// vmCompanionTestUsageCSV is one full day of usage for det-vm/vm-recs on
// 2026-10-25 (the ingestion suite's fixture shape).
func vmCompanionTestUsageCSV() string {
	var b strings.Builder
	b.WriteString(ingestion.CanonicalVMUsageCSVHeader())
	b.WriteByte('\n')
	for i := 0; i < 96; i++ {
		start := fmt.Sprintf("2026-10-25T%02d:%02d:00Z", i/4, (i%4)*15)
		end := "2026-10-26T00:00:00Z"
		if i+1 < 96 {
			end = fmt.Sprintf("2026-10-25T%02d:%02d:00Z", (i+1)/4, ((i+1)%4)*15)
		}
		fmt.Fprintf(&b, "%s,%s,det-vm,vm-recs,worker-1,linux,1500,2000,4000,1048576,4194304,1572864,107374182400,53687091200,107374182400,120,80,1048576,524288\n",
			start, end)
	}
	return b.String()
}

func vmCompanionTestPVCCSV() string {
	return `interval_start,interval_end,vm_name,namespace,node_name,pvc_name,disk_capacity_bytes,volume_mode
2026-10-25T12:00:00Z,2026-10-25T12:15:00Z,det-vm,vm-recs,worker-1,data-pvc,107374182400,Filesystem
2026-10-25T12:00:00Z,2026-10-25T12:15:00Z,det-vm,vm-recs,worker-1,logs-pvc,53687091200,Filesystem
`
}

func vmCompanionTestGPUCSV() string {
	return ingestion.CanonicalVMGPUDeviceCSVHeader() + `
2026-10-25T12:00:00Z,vm-recs,det-vm,gpu-aaa,A100,0.10,0.25,1024,2048,0.05,0.02,0.03,1g.5gb,7
2026-10-25T12:00:00Z,vm-recs,det-vm,gpu-bbb,H100,0.20,0.40,2048,4096,0.06,0.03,0.04,2g.10gb,4
`
}
