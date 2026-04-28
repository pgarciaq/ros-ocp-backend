package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// These tests verify that ProcessReport handles poison messages (invalid JSON,
// unmarshal failures, validation failures) gracefully — returning early without
// panicking or entering an infinite loop. The nil consumer is safe because
// commitOnPermanentFailure guards against it.

func TestProcessReport_InvalidJSON_ReturnsEarly(t *testing.T) {
	msg := &kafka.Message{
		Value:          []byte("{not valid json!!!"),
		TopicPartition: kafka.TopicPartition{Partition: 0},
	}
	ProcessReport(msg, nil)
}

func TestProcessReport_UnmarshalError_ReturnsEarly(t *testing.T) {
	msg := &kafka.Message{
		Value:          []byte(`{"unexpected_field": 42}`),
		TopicPartition: kafka.TopicPartition{Partition: 0},
	}
	ProcessReport(msg, nil)
}

func TestProcessReport_ValidationError_ReturnsEarly(t *testing.T) {
	msg := &kafka.Message{
		Value:          []byte(`{"request_id":"","b64_identity":"","metadata":{},"files":[]}`),
		TopicPartition: kafka.TopicPartition{Partition: 0},
	}
	ProcessReport(msg, nil)
}

func TestProcessContainerCSVNative_EndToEnd(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-native-test"
	clusterUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	csvData := buildTestCSV(7)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csvData)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-req-1",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-test"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "test-cluster"

	processContainerCSVNative(ts.URL, kafkaMsg)

	var digestCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&digestCount)
	require.NoError(t, err)
	assert.Greater(t, digestCount, 0, "should have inserted digests")

	var recCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "should have written recommendations")
}

func TestProcessContainerCSVNative_HTTPFailure(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-req-2",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL},
	}
	kafkaMsg.Metadata.Org_id = "org-fail"
	kafkaMsg.Metadata.Source_id = "src-fail"
	kafkaMsg.Metadata.Cluster_uuid = "ffffffff-ffff-ffff-ffff-ffffffffffff"
	kafkaMsg.Metadata.Cluster_alias = "fail-cluster"

	processContainerCSVNative(ts.URL, kafkaMsg)

	ctx := context.Background()
	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`, "org-fail").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "no digests should be written on HTTP failure")
}

func TestProcessContainerCSVNative_EmptyCSV(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, "")
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-req-3",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL},
	}
	kafkaMsg.Metadata.Org_id = "org-empty"
	kafkaMsg.Metadata.Source_id = "src-empty"
	kafkaMsg.Metadata.Cluster_uuid = "00000000-0000-0000-0000-000000000000"
	kafkaMsg.Metadata.Cluster_alias = "empty-cluster"

	processContainerCSVNative(ts.URL, kafkaMsg)

	ctx := context.Background()
	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`, "org-empty").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "no digests should be written for empty CSV")
}

func TestProcessContainerCSVNative_WithOOMData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-oom-test"
	clusterUUID := "11111111-2222-3333-4444-555555555555"

	csvData := buildTestCSVWithOOM(7)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csvData)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-req-oom",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-oom"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "oom-cluster"

	processContainerCSVNative(ts.URL, kafkaMsg)

	var oomDigestSum int64
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(oom_count_sum), 0) FROM daily_container_digests
		 WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'oom-container'`,
		orgID, clusterUUID).Scan(&oomDigestSum)
	require.NoError(t, err)
	assert.Greater(t, oomDigestSum, int64(0), "oom-container digests should have OOM events")

	var noOomDigestSum int64
	err = pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(oom_count_sum), 0) FROM daily_container_digests
		 WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'stable-container'`,
		orgID, clusterUUID).Scan(&noOomDigestSum)
	require.NoError(t, err)
	assert.Equal(t, int64(0), noOomDigestSum, "stable-container should have zero OOM events")

	var oomMemReq, stableMemReq int64
	err = pool.QueryRow(ctx,
		`SELECT COALESCE(rec_memory_request_kib, 0)
		 FROM recommendation_sets
		 WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'oom-container'
		   AND term = 'medium' AND engine = 'cost'
		 LIMIT 1`,
		orgID, clusterUUID).Scan(&oomMemReq)
	require.NoError(t, err)

	err = pool.QueryRow(ctx,
		`SELECT COALESCE(rec_memory_request_kib, 0)
		 FROM recommendation_sets
		 WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'stable-container'
		   AND term = 'medium' AND engine = 'cost'
		 LIMIT 1`,
		orgID, clusterUUID).Scan(&stableMemReq)
	require.NoError(t, err)

	assert.Greater(t, oomMemReq, stableMemReq,
		"OOM container memory recommendation should be higher than stable container")

	var qualityCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_quality WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&qualityCount)
	require.NoError(t, err)
	assert.Greater(t, qualityCount, 0, "quality metrics should be written")

	// Verify oom_events_after_rec is populated correctly per container
	var oomEvents int64
	err = pool.QueryRow(ctx,
		`SELECT oom_events_after_rec FROM recommendation_quality
		 WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'oom-container'
		 ORDER BY measured_at DESC LIMIT 1`,
		orgID, clusterUUID).Scan(&oomEvents)
	require.NoError(t, err)
	assert.Greater(t, oomEvents, int64(0), "oom-container quality should have positive OOM events")

	var stableOomEvents int64
	err = pool.QueryRow(ctx,
		`SELECT oom_events_after_rec FROM recommendation_quality
		 WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'stable-container'
		 ORDER BY measured_at DESC LIMIT 1`,
		orgID, clusterUUID).Scan(&stableOomEvents)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stableOomEvents, "stable-container quality should have zero OOM events")

	// Verify OOM notification code is present for the OOM container
	var notifCodes []int16
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT unnest(notification_codes)
		 FROM recommendation_sets
		 WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'oom-container'`,
		orgID, clusterUUID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var code int16
		require.NoError(t, rows.Scan(&code))
		notifCodes = append(notifCodes, code)
	}
	require.NoError(t, rows.Err())
	assert.Contains(t, notifCodes, engine.NotifOOMDetected,
		"oom-container should have NotifOOMDetected (code 3) in notification_codes")
}

func TestProcessContainerCSVNative_NoOOMColumn(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-no-oom-col"
	clusterUUID := "22222222-3333-4444-5555-666666666666"

	csvData := buildTestCSVWithoutOOMColumn(7)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		fmt.Fprint(w, csvData)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "test-req-no-oom",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL},
	}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Source_id = "src-no-oom"
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID
	kafkaMsg.Metadata.Cluster_alias = "no-oom-cluster"

	processContainerCSVNative(ts.URL, kafkaMsg)

	var digestCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&digestCount)
	require.NoError(t, err)
	assert.Greater(t, digestCount, 0, "digests should be written even without oom_count column")

	var oomSum int64
	err = pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(oom_count_sum), 0) FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&oomSum)
	require.NoError(t, err)
	assert.Equal(t, int64(0), oomSum, "oom_count_sum should default to 0 when column is absent")

	var recCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "recommendations should still be generated without oom_count")
}

const testCSVHeader = "interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count"

const testCSVHeaderNoOOM = "interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg"

func buildTestCSV(days int) string {
	var sb strings.Builder
	sb.WriteString(testCSVHeader)
	sb.WriteByte('\n')

	now := time.Now().UTC()
	for d := 0; d < days; d++ {
		day := now.AddDate(0, 0, -(days - d))
		for h := 0; h < 24; h++ {
			for q := 0; q < 4; q++ {
				start := time.Date(day.Year(), day.Month(), day.Day(), h, q*15, 0, 0, time.UTC)
				end := start.Add(15 * time.Minute)
				sb.WriteString(fmt.Sprintf("%s,%s,test-ns,pod-t,test-deploy,deployment,main,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000,0\n",
					start.Format("2006-01-02 15:04:05 +0000 UTC"),
					end.Format("2006-01-02 15:04:05 +0000 UTC"),
				))
			}
		}
	}
	return sb.String()
}

// buildTestCSVWithOOM generates CSV data with two containers: one with OOM events,
// one without. Both have identical resource usage so the only difference in memory
// recommendations comes from the OOM bump.
func buildTestCSVWithOOM(days int) string {
	var sb strings.Builder
	sb.WriteString(testCSVHeader)
	sb.WriteByte('\n')

	now := time.Now().UTC()
	for d := 0; d < days; d++ {
		day := now.AddDate(0, 0, -(days - d))
		for h := 0; h < 24; h++ {
			for q := 0; q < 4; q++ {
				start := time.Date(day.Year(), day.Month(), day.Day(), h, q*15, 0, 0, time.UTC)
				end := start.Add(15 * time.Minute)
				ts := start.Format("2006-01-02 15:04:05 +0000 UTC")
				te := end.Format("2006-01-02 15:04:05 +0000 UTC")

				oomVal := 0
				if h%6 == 0 && q == 0 {
					oomVal = 1
				}
				sb.WriteString(fmt.Sprintf("%s,%s,oom-ns,pod-oom,oom-deploy,deployment,oom-container,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000,%d\n", ts, te, oomVal))
				sb.WriteString(fmt.Sprintf("%s,%s,stable-ns,pod-st,stable-deploy,deployment,stable-container,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000,0\n", ts, te))
			}
		}
	}
	return sb.String()
}

// buildTestCSVWithoutOOMColumn generates CSV with the same data as buildTestCSV
// but omits the oom_count column entirely, simulating data from an older operator.
func buildTestCSVWithoutOOMColumn(days int) string {
	var sb strings.Builder
	sb.WriteString(testCSVHeaderNoOOM)
	sb.WriteByte('\n')

	now := time.Now().UTC()
	for d := 0; d < days; d++ {
		day := now.AddDate(0, 0, -(days - d))
		for h := 0; h < 24; h++ {
			for q := 0; q < 4; q++ {
				start := time.Date(day.Year(), day.Month(), day.Day(), h, q*15, 0, 0, time.UTC)
				end := start.Add(15 * time.Minute)
				sb.WriteString(fmt.Sprintf("%s,%s,test-ns,pod-t,test-deploy,deployment,main,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000\n",
					start.Format("2006-01-02 15:04:05 +0000 UTC"),
					end.Format("2006-01-02 15:04:05 +0000 UTC"),
				))
			}
		}
	}
	return sb.String()
}
