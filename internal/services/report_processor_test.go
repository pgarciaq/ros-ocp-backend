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

const testCSVHeader = "interval_start,interval_end,namespace,workload_name,workload_type,container_name,cpu_request,cpu_limit,cpu_usage,cpu_throttle,mem_request,mem_limit,mem_usage,mem_rss,oom_count"

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
				sb.WriteString(fmt.Sprintf("%s,%s,test-ns,test-deploy,deployment,main,0.1,0.15,0.08,0.001,134217728,134217728,104857600,100000000,0\n",
					start.Format("2006-01-02 15:04:05 +0000 UTC"),
					end.Format("2006-01-02 15:04:05 +0000 UTC"),
				))
			}
		}
	}
	return sb.String()
}
