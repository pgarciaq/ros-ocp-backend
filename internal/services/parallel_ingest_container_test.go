package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// #758 (V15-08), services level: a manifest whose container files carry
// disjoint portions of one container/day must produce the complete daily
// distribution AND mark both files done. The baseline processed each file
// through its own digest stream with a replacement upsert — both files done,
// sample_count=1 (the audit's numbers).

func TestParallelIngestFiles_ContainerFilesSameDayAccumulate(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-ctr-batch"
	clusterUUID := "10101010-1111-2222-3333-444444444444"

	// The audit's fixture: 08:00=2000m in one file, 09:00=100m in another,
	// same container/day.
	fileA := containerFixtureCSV("08", "2.0")
	fileB := containerFixtureCSV("09", "0.1")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		switch r.URL.Path {
		case "/ocp_ros_usage_1.csv":
			fmt.Fprint(w, fileA)
		case "/ocp_ros_usage_2.csv":
			fmt.Fprint(w, fileB)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-ctr-batch",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_usage_1.csv",
			ts.URL + "/ocp_ros_usage_2.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-ctr-batch"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "ctr-batch-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-ctr-batch-1"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)

	manifestID := kafkaMsg.Metadata.Manifest_id
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, manifestID,
		clusterUUID, orgID, kafkaMsg.Files, reportTypeForFilename))

	transientErr, permFailed := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, manifestID, true, &rhAccount, &cluster)
	require.NoError(t, transientErr, "no transient error expected")
	assert.False(t, permFailed, "no permanent failure expected")

	for _, filename := range []string{"ocp_ros_usage_1.csv", "ocp_ros_usage_2.csv"} {
		status, err := model.GetReportFileStatus(ctx, pool, manifestID, filename)
		require.NoError(t, err)
		assert.Equal(t, model.ReportFileDone, status, "%s must be done", filename)
	}

	// THE #758 ASSERTION: both files done AND the complete distribution.
	var count int
	var cpuMax int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT sample_count, cpu_usage_max_mc FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'ns-1' AND workload = 'wl-1'
			AND container_name = 'ctr-1' AND bucket_date = '2026-10-25' AND schedule_type = 'all_hours'`,
		orgID, clusterUUID).Scan(&count, &cpuMax))
	assert.Equal(t, 2, count, "both same-day observations must survive the manifest (#758)")
	assert.Equal(t, int64(2000), cpuMax, "the high point must survive the manifest (#758)")

	// Same-manifest replay: done files skip, digests unchanged.
	transientErr, permFailed = parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, manifestID, true, &rhAccount, &cluster)
	assert.NoError(t, transientErr, "replay is a clean no-op")
	assert.False(t, permFailed)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT sample_count FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'ns-1' AND container_name = 'ctr-1'
			AND bucket_date = '2026-10-25' AND schedule_type = 'all_hours'`,
		orgID, clusterUUID).Scan(&count))
	assert.Equal(t, 2, count, "replay must not duplicate or lose samples (#758)")
}

// F4 (review finding): the crash window between per-file done-marks — file
// A done, file B not — must re-process the WHOLE set, not digest B alone
// and replace A+B's distribution.
func TestParallelIngestFiles_PartialDoneReplayReprocessesWholeBatch(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })
	config.ResetForTest()
	_ = config.GetConfig()

	orgID := "org-ctr-crash"
	clusterUUID := "12121212-1111-2222-3333-444444444444"

	fileA := containerFixtureCSV("08", "2.0")
	fileB := containerFixtureCSV("09", "0.1")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		switch r.URL.Path {
		case "/ocp_ros_usage_1.csv":
			fmt.Fprint(w, fileA)
		case "/ocp_ros_usage_2.csv":
			fmt.Fprint(w, fileB)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-ctr-crash",
		B64_identity: "dGVzdA==",
		Files: []string{
			ts.URL + "/ocp_ros_usage_1.csv",
			ts.URL + "/ocp_ros_usage_2.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-ctr-crash"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "ctr-crash-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-ctr-crash-1"

	rhAccount, cluster := setupTestAccountAndCluster(t, pool, orgID, clusterUUID)
	manifestID := kafkaMsg.Metadata.Manifest_id
	require.NoError(t, model.EnsureReportFileExpectations(ctx, pool, manifestID,
		clusterUUID, orgID, kafkaMsg.Files, reportTypeForFilename))

	// First attempt: complete.
	transientErr, _ := parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, manifestID, true, &rhAccount, &cluster)
	require.NoError(t, transientErr)

	// Simulate the crash window: file A stays done, file B is reset to
	// un-done (as if the process died between the two done-marks).
	require.NoError(t, model.MarkReportFileProcessing(ctx, pool, manifestID, clusterUUID, orgID,
		"ocp_ros_usage_2.csv", string(types.PayloadTypeContainer)))

	// The retry must re-process the WHOLE batch (A is re-ingested too), not
	// digest B alone and replace the complete distribution.
	transientErr, _ = parallelIngestFiles(ctx, pool, logging_entry(), kafkaMsg, manifestID, true, &rhAccount, &cluster)
	require.NoError(t, transientErr)

	var count int
	var cpuMax int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT sample_count, cpu_usage_max_mc FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'ns-1' AND container_name = 'ctr-1'
			AND bucket_date = '2026-10-25' AND schedule_type = 'all_hours'`,
		orgID, clusterUUID).Scan(&count, &cpuMax))
	assert.Equal(t, 2, count, "a partially-done replay must re-digest the whole batch (#758 review finding)")
	assert.Equal(t, int64(2000), cpuMax, "the high point must survive the crash-window replay")
}

// containers stage mid-file; a later row for an ALREADY-STAGED key stays in
// memory, and the finalize must merge staged + in-memory samples for that
// key losslessly — the exact path a count-split manifest exercises.

// containerFixtureCSV mirrors the ingestion suite's fixture: one row for the
// fixture container at the given hour.
func containerFixtureCSV(hour string, cpuUsageCores string) string {
	return "interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count\n" +
		fmt.Sprintf("2026-10-25T%s:00:00Z,2026-10-25T%s:59:59Z,ns-1,pod-1,wl-1,Deployment,ctr-1,1.0,2.0,%s,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n",
			hour, hour, cpuUsageCores)
}
