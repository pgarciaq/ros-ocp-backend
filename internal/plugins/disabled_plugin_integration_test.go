package plugins_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"

	// Registers built-in plugins (avoid importing leaf packages twice — they register via init).
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
)

func ingestHookNames(t *testing.T) []string {
	t.Helper()
	hooks := plugin.ByTrait[plugin.IngestHook]()
	out := make([]string, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, h.Name())
	}
	return out
}

// TestRegistry_DisabledGPUExcludedFromIngestHooks verifies ROS_DISABLED_PLUGINS removes the GPU
// hook from [plugin.ByTrait][plugin.IngestHook] while other hooks (e.g. node) remain.
func TestRegistry_DisabledGPUExcludedFromIngestHooks(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")

	t.Setenv("ROS_DISABLED_PLUGINS", "gpu")
	off := ingestHookNames(t)
	assert.NotContains(t, off, "gpu", "gpu IngestHook should be omitted when ROS_DISABLED_PLUGINS=gpu")
	assert.Contains(t, off, "node", "node IngestHook should remain enabled")

	t.Setenv("ROS_DISABLED_PLUGINS", "")
	on := ingestHookNames(t)
	assert.Contains(t, on, "gpu", "gpu IngestHook should register when not blocklisted")
}

// gpuContainerCSV is a minimal container report row including GPU columns so [ingestion.MetricRow.HasGPU] is true.
// Format matches [internal/ingestion.TestParseCSVRows_GPUMetrics].
const gpuContainerCSV = "" +
	"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
	"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
	"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count," +
	"accelerator_core_usage_percentage_min,accelerator_model_name,accelerator_profile_name," +
	"accelerator_frame_buffer_usage_min,accelerator_frame_buffer_usage_max,accelerator_frame_buffer_usage_avg," +
	"tensor_pipe_active_min,tensor_pipe_active_max,tensor_pipe_active_avg," +
	"dram_active_min,dram_active_max,dram_active_avg," +
	"sm_active_min,sm_active_max,sm_active_avg\n" +
	"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-gpu,train,deployment,app," +
	"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0," +
	",NVIDIA-A100-SXM4-80GB,3g.40gb," +
	"1024.5,2048.0,1536.25," +
	"0.1,0.2,0.15," +
	"0.3,0.4,0.35," +
	"0.5,0.6,0.55\n"

// dispatchContainerIngestAndHooks mirrors services.nativeCSVIngestViaPlugins for csvType "container".
func dispatchContainerIngestAndHooks(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, r *strings.Reader) error {
	ingestors := plugin.ByTrait[plugin.CSVIngestor]()
	var matched plugin.CSVIngestor
	for _, ing := range ingestors {
		for _, tname := range ing.SupportedCSVTypes() {
			if tname == "container" {
				matched = ing
				break
			}
		}
		if matched != nil {
			break
		}
	}
	if matched == nil {
		return nil
	}
	rows, err := matched.IngestCSV(ctx, pool, r, orgID, clusterUUID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	hooks := plugin.ByTrait[plugin.IngestHook]()
	for _, hook := range hooks {
		for _, ht := range hook.HookAfterCSVTypes() {
			if ht == "container" {
				if hookErr := hook.AfterIngest(ctx, pool, rows, orgID, clusterUUID); hookErr != nil {
					return hookErr
				}
				break
			}
		}
	}
	return nil
}

// TestDisabledGPUPlugin_NoGPUDigestRows uses testcontainers (skipped under -short) to prove that when the GPU
// plugin is blocklisted, container CSV processing still writes container digests but does not populate gpu_container_digests.
func TestDisabledGPUPlugin_NoGPUDigestRows(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "gpu")

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-disabled-gpu-plugin-test"
	clusterUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	r := strings.NewReader(gpuContainerCSV)
	require.NoError(t, dispatchContainerIngestAndHooks(ctx, pool, orgID, clusterUUID, r))

	var containerDigests int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`,
		orgID).Scan(&containerDigests)
	require.NoError(t, err)
	assert.Greater(t, containerDigests, 0, "container CSVIngestor should still persist daily_container_digests")

	var gpuDigests int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM gpu_container_digests WHERE cluster_uuid = $1`,
		clusterUUID).Scan(&gpuDigests)
	require.NoError(t, err)
	assert.Equal(t, 0, gpuDigests, "GPU IngestHook disabled — expect no gpu_container_digests rows")
}
