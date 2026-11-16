package services

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// DB-backed proof that DLQ exhaustion moves files out of non-terminal
// report_file_status states (closes the unit-test residual: pure unit tests
// cannot observe the MarkReportFileFailed write).
func TestHandleKafkaTransientError_DLQExhaustionMarksFilesFailed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "dlq-test-req",
		B64_identity: "test",
		Files: []string{
			"http://localhost:8888/dlq/May-2026-550e8400-ocp_ros_usage.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = "dlq-org"
	kafkaMsg.Metadata.Source_id = "dlq-src"
	kafkaMsg.Metadata.Cluster_uuid = "550e8400-e29b-41d4-a716-446655440001"
	kafkaMsg.Metadata.Cluster_alias = "dlq-cluster"
	kafkaMsg.Metadata.Manifest_id = "dlq-manifest-1"
	require.NoError(t, ensureManifestExpectations(ctx, pool, kafkaMsg))

	value, err := json.Marshal(kafkaMsg)
	require.NoError(t, err)
	topic := "hccm.ros.events"
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: 0},
		Key:            []byte("dlq-key"),
		Value:          value,
		Headers: []kafka.Header{
			{Key: retryCountHeader, Value: []byte(strconv.Itoa(maxTransientRetries()))},
			{Key: dlqAttemptHeader, Value: []byte(strconv.Itoa(defaultMaxDLQAttempts))},
		},
	}

	statusBefore, err := model.GetReportFileStatus(ctx, pool, "dlq-manifest-1", "May-2026-550e8400-ocp_ros_usage.csv")
	require.NoError(t, err)
	assert.NotEqual(t, model.ReportFileFailed, statusBefore)

	producer := &fakeKafkaProducer{err: assert.AnError}
	consumer := &fakeKafkaCommitter{}
	handleKafkaTransientError(consumer, producer, msg, assert.AnError)

	assert.True(t, consumer.committed)
	statusAfter, err := model.GetReportFileStatus(ctx, pool, "dlq-manifest-1", "May-2026-550e8400-ocp_ros_usage.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileFailed, statusAfter)
}

// TestHandleKafkaTransientError_ProduceFailureMarksFilesFailed pins the
// #803 review's finding 3: the files-failed marker must fire for the
// retry-produce fallback too, not only the DLQ-exhaustion path — a
// regression dropping the marker from either fallback would otherwise
// ship green.
func TestHandleKafkaTransientError_ProduceFailureMarksFilesFailed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "fallback-req-retry",
		B64_identity: "test",
		Files: []string{
			"http://localhost:8888/dlq/May-2026-550e8400-ocp_ros_usage.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = "dlq-org"
	kafkaMsg.Metadata.Source_id = "dlq-src"
	kafkaMsg.Metadata.Cluster_uuid = "550e8400-e29b-41d4-a716-446655440001"
	kafkaMsg.Metadata.Cluster_alias = "dlq-cluster"
	kafkaMsg.Metadata.Manifest_id = "fallback-manifest-retry"
	require.NoError(t, ensureManifestExpectations(ctx, pool, kafkaMsg))

	value, err := json.Marshal(kafkaMsg)
	require.NoError(t, err)
	topic := "hccm.ros.events"
	// No retry header: the message is on its first transient failure — the
	// retry-produce path, whose failure is this fallback.
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: 0},
		Key:            []byte("fallback-key"),
		Value:          value,
	}

	producer := &fakeKafkaProducer{err: assert.AnError}
	consumer := &fakeKafkaCommitter{}
	handleKafkaTransientError(consumer, producer, msg, assert.AnError)

	assert.True(t, consumer.committed, "the retry-produce fallback must commit (#803)")
	statusAfter, err := model.GetReportFileStatus(ctx, pool, "fallback-manifest-retry", "May-2026-550e8400-ocp_ros_usage.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileFailed, statusAfter,
		"the retry-produce fallback must mark the files failed (#803 review finding 3)")
}

// TestHandleKafkaTransientError_DLQRedeliveryFailureMarksFilesFailed pins
// the marker for the other #803 fallback (DLQ produce + DLQ-redelivery
// produce both failing at retry exhaustion).
func TestHandleKafkaTransientError_DLQRedeliveryFailureMarksFilesFailed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "fallback-req-dlq",
		B64_identity: "test",
		Files: []string{
			"http://localhost:8888/dlq/May-2026-550e8400-ocp_ros_usage.csv",
		},
	}
	kafkaMsg.Metadata.Org_id = "dlq-org"
	kafkaMsg.Metadata.Source_id = "dlq-src"
	kafkaMsg.Metadata.Cluster_uuid = "550e8400-e29b-41d4-a716-446655440001"
	kafkaMsg.Metadata.Cluster_alias = "dlq-cluster"
	kafkaMsg.Metadata.Manifest_id = "fallback-manifest-dlq"
	require.NoError(t, ensureManifestExpectations(ctx, pool, kafkaMsg))

	value, err := json.Marshal(kafkaMsg)
	require.NoError(t, err)
	topic := "hccm.ros.events"
	// Retry-exhausted (drives the DLQ path) with DLQ attempts remaining —
	// the DLQ produce and the DLQ-redelivery produce both fail.
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: 0},
		Key:            []byte("fallback-key"),
		Value:          value,
		Headers:        []kafka.Header{{Key: retryCountHeader, Value: []byte(strconv.Itoa(maxTransientRetries()))}},
	}

	producer := &fakeKafkaProducer{err: assert.AnError}
	consumer := &fakeKafkaCommitter{}
	handleKafkaTransientError(consumer, producer, msg, assert.AnError)

	assert.True(t, consumer.committed, "the DLQ-redelivery-failure fallback must commit (#803)")
	statusAfter, err := model.GetReportFileStatus(ctx, pool, "fallback-manifest-dlq", "May-2026-550e8400-ocp_ros_usage.csv")
	require.NoError(t, err)
	assert.Equal(t, model.ReportFileFailed, statusAfter,
		"the DLQ-redelivery-failure fallback must mark the files failed (#803 review finding 3)")
}
