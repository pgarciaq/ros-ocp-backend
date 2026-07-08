package plugins_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"

	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins" // registers all plugins
)

const containerCSVWithGPU = "" +
	"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
	"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
	"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count," +
	"accelerator_core_usage_percentage_min,accelerator_model_name,accelerator_profile_name," +
	"accelerator_frame_buffer_usage_min,accelerator_frame_buffer_usage_max,accelerator_frame_buffer_usage_avg," +
	"tensor_pipe_active_min,tensor_pipe_active_max,tensor_pipe_active_avg," +
	"dram_active_min,dram_active_max,dram_active_avg," +
	"sm_active_min,sm_active_max,sm_active_avg\n" +
	"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,ml-training,pod-gpu-1,train-job,deployment,trainer," +
	"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0," +
	",NVIDIA-A100-SXM4-80GB,3g.40gb," +
	"1024.5,2048.0,1536.25," +
	"0.1,0.2,0.15," +
	"0.3,0.4,0.35," +
	"0.5,0.6,0.55\n"

const namespaceCSV = "" +
	"interval_start,interval_end,namespace," +
	"cpu_request_namespace_sum,cpu_usage_namespace_avg," +
	"memory_request_namespace_sum,memory_usage_namespace_avg\n" +
	"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,web-app," +
	"2000.0,800.0,4194304.0,2097152.0\n" +
	"2026-03-01 00:15:00 +0000 UTC,2026-03-01 00:30:00 +0000 UTC,web-app," +
	"2000.0,900.0,4194304.0,2200000.0\n"

// TestPluginLifecycle_ContainerCSVToDigests verifies the full plugin-dispatched
// lifecycle: CSV reader → CSVIngestor plugin → daily_container_digests rows in DB.
func TestPluginLifecycle_ContainerCSVToDigests(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-plugin-lifecycle-container"
	clusterUUID := "11111111-2222-3333-4444-555555555555"

	ingestors := plugin.ByTrait[plugin.CSVIngestor]()
	var matched plugin.CSVIngestor
	for _, ing := range ingestors {
		for _, csvType := range ing.SupportedCSVTypes() {
			if csvType == "container" {
				matched = ing
				break
			}
		}
		if matched != nil {
			break
		}
	}
	require.NotNil(t, matched, "expected a CSVIngestor for 'container' CSV type")

	rows, err := matched.IngestCSV(ctx, pool, strings.NewReader(containerCSVWithGPU), orgID, clusterUUID)
	require.NoError(t, err)
	_ = rows // streaming ingest persists directly; row slice may be empty

	var containerDigests int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&containerDigests)
	require.NoError(t, err)
	assert.Greater(t, containerDigests, 0, "container plugin should persist daily_container_digests")

	return
}

// TestPluginLifecycle_GPUIngestHookWritesDigests verifies that after container
// CSV ingestion, the GPU IngestHook writes to gpu_container_digests.
func TestPluginLifecycle_GPUIngestHookWritesDigests(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-plugin-lifecycle-gpu"
	clusterUUID := "aaaaaaaa-1111-2222-3333-444444444444"

	ingestors := plugin.ByTrait[plugin.CSVIngestor]()
	var matched plugin.CSVIngestor
	for _, ing := range ingestors {
		for _, csvType := range ing.SupportedCSVTypes() {
			if csvType == "container" {
				matched = ing
				break
			}
		}
		if matched != nil {
			break
		}
	}
	require.NotNil(t, matched)

	_, err := matched.IngestCSV(ctx, pool, strings.NewReader(containerCSVWithGPU), orgID, clusterUUID)
	require.NoError(t, err)

	var gpuDigests int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM gpu_container_digests WHERE cluster_uuid = $1`,
		clusterUUID).Scan(&gpuDigests)
	require.NoError(t, err)
	assert.Greater(t, gpuDigests, 0, "container ingest stream should persist gpu_container_digests when GPU plugin is enabled")
}

// TestPluginLifecycle_NamespaceCSVToDigests verifies the namespace plugin's
// full lifecycle: CSV → CSVIngestor → daily_namespace_digests.
func TestPluginLifecycle_NamespaceCSVToDigests(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-plugin-lifecycle-ns"
	clusterUUID := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"

	ingestors := plugin.ByTrait[plugin.CSVIngestor]()
	var matched plugin.CSVIngestor
	for _, ing := range ingestors {
		for _, csvType := range ing.SupportedCSVTypes() {
			if csvType == "namespace" {
				matched = ing
				break
			}
		}
		if matched != nil {
			break
		}
	}
	require.NotNil(t, matched, "expected a CSVIngestor for 'namespace' CSV type")

	_, err := matched.IngestCSV(ctx, pool, strings.NewReader(namespaceCSV), orgID, clusterUUID)
	require.NoError(t, err)

	var nsDigests int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_namespace_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&nsDigests)
	require.NoError(t, err)
	assert.Greater(t, nsDigests, 0, "namespace plugin should persist daily_namespace_digests")
}

// TestPluginLifecycle_EndToEnd_FullDispatch uses plugin.DispatchCSV — the same
// function production calls — to validate the full CSV→DB lifecycle in one shot.
func TestPluginLifecycle_EndToEnd_FullDispatch(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	syncPluginConfigFromEnv(t)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-plugin-lifecycle-e2e"
	clusterUUID := "e2e2e2e2-1111-2222-3333-444444444444"

	handled, _, hookErrs, err := plugin.DispatchCSV(ctx, pool, strings.NewReader(containerCSVWithGPU), orgID, clusterUUID, "container")
	require.NoError(t, err)
	assert.True(t, handled, "container CSVIngestor should claim the type")
	assert.Empty(t, hookErrs, "no hook errors expected for valid GPU data")

	var containerCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&containerCount)
	require.NoError(t, err)
	assert.Greater(t, containerCount, 0)

	var gpuCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM gpu_container_digests WHERE cluster_uuid = $1`,
		clusterUUID).Scan(&gpuCount)
	require.NoError(t, err)
	assert.Greater(t, gpuCount, 0)

	parsedRows, err := ingestion.ParseCSVRows(strings.NewReader(containerCSVWithGPU))
	require.NoError(t, err)
	require.NotEmpty(t, parsedRows)
	assert.True(t, parsedRows[0].HasGPU(), "row with GPU columns should report HasGPU()=true")
	verifyRowShape(t, parsedRows[0])
}

func verifyRowShape(t *testing.T, row ingestion.MetricRow) {
	t.Helper()
	assert.NotEmpty(t, row.Namespace)
	assert.NotEmpty(t, row.WorkloadName)
	assert.NotEmpty(t, row.ContainerName)
	assert.False(t, row.IntervalStart.IsZero())
	assert.False(t, row.IntervalEnd.IsZero())
}
