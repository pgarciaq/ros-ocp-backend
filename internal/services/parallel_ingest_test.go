package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestParallelIngestFiles_AllFilesProcessed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	t.Setenv("ROS_MANIFEST_DOWNLOAD_WORKERS", "3")
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-parallel-ok"
	clusterUUID := "aaaaaaaa-1111-2222-3333-444444444444"

	csvData := buildTestCSV(3)
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csvData)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-parallel-ok",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL + "/ocp_ros_usage_file1.csv", ts.URL + "/ocp_ros_usage_file2.csv", ts.URL + "/ocp_ros_usage_file3.csv"},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-parallel"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "parallel-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-parallel-ok"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	transientErr, permFailed := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, kafkaMsg.Metadata.Manifest_id, true, &rhAccount, &cluster)

	assert.NoError(t, transientErr, "no transient error expected")
	assert.False(t, permFailed, "no permanent failure expected")
	assert.Equal(t, int32(3), requestCount.Load(), "all 3 files should be fetched")
}

func TestParallelIngestFiles_UnrecognizedFileSkippedOthersProcessed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	t.Setenv("ROS_MANIFEST_DOWNLOAD_WORKERS", "3")
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-parallel-skip"
	clusterUUID := "bbbbbbbb-1111-2222-3333-444444444444"

	csvData := buildTestCSV(3)
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csvData)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-parallel-skip",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_usage_good1.csv",
			ts.URL + "/unknown_type.txt",
			ts.URL + "/ocp_ros_usage_good2.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-skip"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "skip-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-parallel-skip"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	transientErr, permFailed := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, kafkaMsg.Metadata.Manifest_id, true, &rhAccount, &cluster)

	assert.NoError(t, transientErr, "unrecognized file should not produce transient error")
	assert.False(t, permFailed, "unrecognized file is silently skipped, not a permanent failure")
}

func TestParallelIngestFiles_TransientErrorFromBadCSVCancelsOthers(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	t.Setenv("ROS_MANIFEST_DOWNLOAD_WORKERS", "1")
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-parallel-badcsv"
	clusterUUID := "bbbbbbbb-2222-3333-4444-555555555555"

	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if strings.Contains(r.URL.Path, "bad") {
			w.Header().Set("Content-Type", "text/csv")
			fmt.Fprint(w, "invalid,csv,without,proper,columns\n")
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, buildTestCSV(3))
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-parallel-badcsv",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_usage_bad.csv",
			ts.URL + "/ocp_ros_usage_good1.csv",
			ts.URL + "/ocp_ros_usage_good2.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-badcsv"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "badcsv-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-parallel-badcsv"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	transientErr, _ := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, kafkaMsg.Metadata.Manifest_id, true, &rhAccount, &cluster)

	assert.Error(t, transientErr, "bad CSV content triggers transient error (conservative error classification)")
}

func TestParallelIngestFiles_TransientErrorCancelsOthers(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	t.Setenv("ROS_MANIFEST_DOWNLOAD_WORKERS", "1")
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-parallel-trans"
	clusterUUID := "cccccccc-1111-2222-3333-444444444444"

	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if strings.Contains(r.URL.Path, "timeout") {
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, buildTestCSV(3))
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-parallel-trans",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_usage_timeout.csv",
			ts.URL + "/ocp_ros_usage_good1.csv",
			ts.URL + "/ocp_ros_usage_good2.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-trans"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "trans-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-parallel-trans"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	transientErr, _ := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, kafkaMsg.Metadata.Manifest_id, true, &rhAccount, &cluster)

	assert.Error(t, transientErr, "should report a transient error for 503")
}

func TestParallelIngestFiles_SingleFileSkipsGoroutine(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-parallel-single"
	clusterUUID := "dddddddd-1111-2222-3333-444444444444"

	csvData := buildTestCSV(3)
	var requestCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csvData)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-parallel-single",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL + "/ocp_ros_usage_single.csv"},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-single"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "single-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-single"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	transientErr, permFailed := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, kafkaMsg.Metadata.Manifest_id, true, &rhAccount, &cluster)

	assert.NoError(t, transientErr)
	assert.False(t, permFailed)
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestParallelIngestFiles_CancelledContext(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-parallel-cancel"
	clusterUUID := "eeeeeeee-1111-2222-3333-444444444444"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, buildTestCSV(3))
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-parallel-cancel",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL + "/ocp_ros_usage_a.csv", ts.URL + "/ocp_ros_usage_b.csv"},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-cancel"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "cancel-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-cancel"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	transientErr, _ := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, kafkaMsg.Metadata.Manifest_id, true, &rhAccount, &cluster)
	assert.Error(t, transientErr, "should return error for cancelled context")
}

func TestConfigValidationWarnings_ManifestDownloadPoolExhaustion(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_MANIFEST_DOWNLOAD_WORKERS", "4")
	t.Setenv("ROS_KAFKA_WORKERS", "3")
	t.Setenv("ROS_DB_MAX_CONNS", "5")
	cfg := config.GetConfig()

	warnings := config.ConfigValidationWarnings(cfg)

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "ROS_MANIFEST_DOWNLOAD_WORKERS") && strings.Contains(w, "connection pool exhaustion") {
			found = true
			break
		}
	}
	assert.True(t, found, "should emit pool exhaustion warning when workers × kafka > max_conns - 2")
}

func TestConfigValidationWarnings_NoPoolWarningWhenSafe(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_MANIFEST_DOWNLOAD_WORKERS", "2")
	t.Setenv("ROS_KAFKA_WORKERS", "3")
	t.Setenv("ROS_DB_MAX_CONNS", "10")
	t.Setenv("DEVELOPMENT", "true")
	cfg := config.GetConfig()

	warnings := config.ConfigValidationWarnings(cfg)

	for _, w := range warnings {
		if strings.Contains(w, "connection pool exhaustion") {
			t.Errorf("should NOT emit pool warning when 2×3=6 <= 10-2=8, but got: %s", w)
		}
	}
}

// --- helpers ---

func setupTestAccountAndCluster(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) (model.RHAccount, model.Cluster) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)

	var accountID uint
	err = pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&accountID)
	require.NoError(t, err)

	sourceID := "src-" + orgID
	alias := "alias-" + orgID
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, source_id, cluster_uuid, cluster_alias, last_reported_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (tenant_id, source_id, cluster_uuid, cluster_alias) DO NOTHING`,
		accountID, sourceID, clusterUUID, alias)
	require.NoError(t, err)

	var clusterID uint
	err = pool.QueryRow(ctx, `SELECT id FROM clusters WHERE tenant_id = $1 AND cluster_uuid = $2`,
		accountID, clusterUUID).Scan(&clusterID)
	require.NoError(t, err)

	return model.RHAccount{ID: accountID, OrgId: orgID},
		model.Cluster{ID: clusterID, TenantID: accountID, ClusterUUID: clusterUUID}
}

func logging_entry() *logrus.Entry {
	return logrus.NewEntry(logrus.StandardLogger())
}

// TestParallelIngestFiles_VMPVCReachesDone proves the #650 routing: a
// vm-pvc manifest file reaches Done via the VM case, the manifest completes,
// and rows land grouped per VM digest — not collapsed by PVC name
// (data-pvc-shared exists on both VMs with different modes/capacities).
func TestParallelIngestFiles_VMPVCReachesDone(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	t.Setenv("ROS_MANIFEST_DOWNLOAD_WORKERS", "3")
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-parallel-vmpvc"
	clusterUUID := "cccccccc-1111-2222-3333-444444444444"

	// Seed host digests so PVC rows attach (IngestVMPVCCSV skips rows with
	// no digest). Minimal NOT NULL columns; schedule_type defaults.
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_vm_digests (org_id, cluster_uuid, vm_name, namespace, bucket_date)
		VALUES ($1, $2, 'db-vm-01', 'vm-ns', '2026-05-01'),
		       ($1, $2, 'db-vm-02', 'vm-ns', '2026-05-01')
		ON CONFLICT (org_id, cluster_uuid, vm_name, namespace, bucket_date, schedule_type) DO NOTHING`,
		orgID, clusterUUID)
	require.NoError(t, err)

	// Nise-shaped fixture (mirrors the verified /tmp/opencode sample):
	// shared PVC name across VMs with distinct modes guards per-VM grouping.
	csvData := `interval_start,interval_end,vm_name,namespace,node_name,pvc_name,disk_capacity_bytes,volume_mode
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,db-vm-01,vm-ns,worker-1,data-pvc-shared,107374182400,Filesystem
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,db-vm-01,vm-ns,worker-1,logs-pvc,53687091200,Filesystem
2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,db-vm-02,vm-ns,worker-2,data-pvc-shared,107374182400,Block
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csvData)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-parallel-vmpvc",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL + "/ros-openshift-vm-pvc-202605.csv"},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-parallel-vmpvc"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "parallel-vmpvc-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-parallel-vmpvc"
	require.NoError(t, ensureManifestExpectations(ctx, pool, kafkaMsg))

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	transientErr, permFailed := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, kafkaMsg.Metadata.Manifest_id, true, &rhAccount, &cluster)

	assert.NoError(t, transientErr, "no transient error expected")
	assert.False(t, permFailed, "no permanent failure expected")
	status, err := model.GetReportFileStatus(ctx, pool, kafkaMsg.Metadata.Manifest_id, "ros-openshift-vm-pvc-202605.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileDone, status, "vm-pvc file must reach Done (pre-#650 it stalled forever)")
	complete, err := model.IsManifestIngestionComplete(ctx, pool, kafkaMsg.Metadata.Manifest_id)
	require.NoError(t, err)
	assert.True(t, complete, "manifest with a vm-pvc file must complete")

	type pvcRow struct {
		VMName, PVCName, Mode string
		Bytes                 int64
	}
	var landed []pvcRow
	rows, err := pool.Query(ctx, `
		SELECT d.vm_name, p.pvc_name, p.disk_capacity_bytes, p.volume_mode
		FROM vm_pvc_digests p JOIN daily_vm_digests d ON d.id = p.vm_digest_id
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 ORDER BY 1, 2`,
		orgID, clusterUUID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var r pvcRow
		require.NoError(t, rows.Scan(&r.VMName, &r.PVCName, &r.Bytes, &r.Mode))
		landed = append(landed, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, landed, 3, "all 3 PVC rows must land grouped per digest, not collapsed by name")
	assert.Contains(t, landed, pvcRow{"db-vm-01", "data-pvc-shared", "Filesystem", 107374182400})
	assert.Contains(t, landed, pvcRow{"db-vm-01", "logs-pvc", "Filesystem", 53687091200})
	assert.Contains(t, landed, pvcRow{"db-vm-02", "data-pvc-shared", "Block", 107374182400})
}

// TestParallelIngestFiles_VMDisabledSkipsDone proves the #650 disabled-plugin
// path: with the vm plugin off, VM files are marked Done (not silently
// stalled) so the manifest completes and other engines proceed.
func TestParallelIngestFiles_VMDisabledSkipsDone(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	t.Setenv("ROS_MANIFEST_DOWNLOAD_WORKERS", "3")
	t.Setenv("ROS_DISABLED_PLUGINS", "vm")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	_ = config.GetConfig()

	orgID := "org-parallel-vmdisabled"
	clusterUUID := "dddddddd-1111-2222-3333-444444444444"

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-parallel-vmdisabled",
		B64_identity: "dGVzdA==",
		Files:        []string{"http://example.invalid/ros-openshift-vm-usage-202605.csv"},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-parallel-vmdisabled"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "parallel-vmdisabled-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-parallel-vmdisabled"
	require.NoError(t, ensureManifestExpectations(ctx, pool, kafkaMsg))

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	transientErr, permFailed := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, kafkaMsg.Metadata.Manifest_id, true, &rhAccount, &cluster)

	assert.NoError(t, transientErr, "no transient error expected")
	assert.False(t, permFailed, "skipped files are not failures")
	status, err := model.GetReportFileStatus(ctx, pool, kafkaMsg.Metadata.Manifest_id, "ros-openshift-vm-usage-202605.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileDone, status, "disabled-plugin files must reach Done (pre-#650 they stalled)")
	complete, err := model.IsManifestIngestionComplete(ctx, pool, kafkaMsg.Metadata.Manifest_id)
	require.NoError(t, err)
	assert.True(t, complete, "manifest must complete without VM data")
}
