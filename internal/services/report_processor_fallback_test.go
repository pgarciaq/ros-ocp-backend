package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// testCSVHeaderGPUNode extends the standard ROS container CSV with node + GPU model columns.
const testCSVHeaderGPUNode = testCSVHeader + ",node,accelerator_model_name"

func buildTestCSVWithGPUAndNode(days int) string {
	var sb strings.Builder
	sb.WriteString(testCSVHeaderGPUNode)
	sb.WriteByte('\n')

	now := time.Now().UTC()
	for d := 0; d < days; d++ {
		day := now.AddDate(0, 0, -(days - d))
		for h := 0; h < 24; h++ {
			for q := 0; q < 4; q++ {
				start := time.Date(day.Year(), day.Month(), day.Day(), h, q*15, 0, 0, time.UTC)
				end := start.Add(15 * time.Minute)
				sb.WriteString(fmt.Sprintf("%s,%s,test-ns,pod-t,test-deploy,deployment,main,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000,0,worker-01,NVIDIA-A100-SXM4-40GB\n",
					start.Format("2006-01-02 15:04:05 +0000 UTC"),
					end.Format("2006-01-02 15:04:05 +0000 UTC"),
				))
			}
		}
	}
	return sb.String()
}

func TestProcessContainerCSVNative_fallbackSkipsGPUAndNodeDigestsWhenDisabled(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "pvc")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-fallback-no-domains"
	clusterUUID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	csv := buildTestCSVWithGPUAndNode(2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csv)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-fallback-1",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-fallback"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "fb-cluster"

	processContainerCSVNative(ts.URL, kafkaMsg)

	var digestCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&digestCount)
	require.NoError(t, err)
	assert.Greater(t, digestCount, 0, "fallback should still ingest container digests")

	var gpuCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM gpu_container_digests WHERE cluster_uuid = $1`, clusterUUID).Scan(&gpuCount)
	require.NoError(t, err)
	assert.Equal(t, 0, gpuCount, "gpu plugin disabled: fallback must not upsert GPU digests")

	var nodeCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_node_digests WHERE cluster_uuid = $1`, clusterUUID).Scan(&nodeCount)
	require.NoError(t, err)
	assert.Equal(t, 0, nodeCount, "node plugin disabled: fallback must not upsert node digests")
}

func TestProcessContainerCSVNative_fallbackUpsertsGPUWhenGPUAllowed(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "pvc,gpu")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-fallback-gpu-on"
	clusterUUID := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	csv := buildTestCSVWithGPUAndNode(2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csv)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-fallback-2",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-fallback-gpu"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "fb-gpu-cluster"

	processContainerCSVNative(ts.URL, kafkaMsg)

	var gpuCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM gpu_container_digests WHERE cluster_uuid = $1`, clusterUUID).Scan(&gpuCount)
	require.NoError(t, err)
	assert.Greater(t, gpuCount, 0, "gpu enabled in allowlist: fallback should upsert GPU digests")
}

func TestProcessContainerCSVNative_pluginPathDoesNotUseFallback(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container,gpu")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-plugin-ingest"
	clusterUUID := "dddddddd-dddd-dddd-dddd-dddddddddddd"

	csv := buildTestCSVWithGPUAndNode(2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csv)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-plugin-ingest",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-plug"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "plugin-cluster"

	processContainerCSVNative(ts.URL, kafkaMsg)

	var gpuCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM gpu_container_digests WHERE cluster_uuid = $1`, clusterUUID).Scan(&gpuCount)
	require.NoError(t, err)
	assert.Greater(t, gpuCount, 0, "container CSVIngestor + gpu hook should populate GPU digests")
}
