package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// #893: the offset-settlement gate had no test in either mode. These pin the
// two modes' contracts so a change of commit semantics is a decision, not an
// accident.

func TestSettleKafkaMessage_ManualCommitOnSuccessCommits(t *testing.T) {
	t.Parallel()
	committer := &fakeKafkaCommitter{}
	producer := &fakeKafkaProducer{}

	ok := settleKafkaMessage(logging_entry(), &config.Config{KafkaAutoCommit: false},
		testTopicMessage(nil), committer, producer, nil, false)

	assert.True(t, ok, "a completed pipeline counts as processed")
	assert.Equal(t, 1, committer.commits, "manual commit must commit the completed offset")
	assert.Empty(t, producer.messages, "no requeue for a successful message")
}

func TestSettleKafkaMessage_ManualCommitOnTransientErrorRequeuesAndCommits(t *testing.T) {
	t.Parallel()
	committer := &fakeKafkaCommitter{}
	producer := &fakeKafkaProducer{}
	msg := testTopicMessage(nil)

	ok := settleKafkaMessage(logging_entry(), &config.Config{KafkaAutoCommit: false},
		msg, committer, producer, assert.AnError, false)

	assert.False(t, ok, "a requeued message is not processed")
	require.Len(t, producer.messages, 1, "the transient failure must go through the retry ladder")
	assert.Equal(t, "hccm.ros.events", *producer.messages[0].TopicPartition.Topic,
		"the retry goes back to the source topic")
	retryCount, found := headerValue(producer.messages[0].Headers, retryCountHeader)
	require.True(t, found, "the retry must carry the retry counter (ADR-0155)")
	assert.Equal(t, "1", retryCount)
	assert.Equal(t, 1, committer.commits,
		"the requeued offset is settled only after the retry was produced")
}

// The auto-commit mode is the defect #893 closes by flipping the deployment
// (KAFKA_AUTO_COMMIT=false). Pinned so a behavior change there is deliberate:
// the tail can neither requeue nor dead-letter, and the offset is acked by
// librdkafka (stored on poll, committed every ~5s) regardless of the outcome.
func TestSettleKafkaMessage_AutoCommitTransientErrorLeavesNoLadder(t *testing.T) {
	t.Parallel()
	committer := &fakeKafkaCommitter{}
	producer := &fakeKafkaProducer{}

	ok := settleKafkaMessage(logging_entry(), &config.Config{KafkaAutoCommit: true},
		testTopicMessage(nil), committer, producer, assert.AnError, false)

	assert.False(t, ok, "the pipeline failed, so it is not counted as processed")
	assert.Empty(t, producer.messages, "auto-commit mode has no retry ladder")
	assert.Zero(t, committer.commits, "auto-commit mode does not commit explicitly")
}

func TestSettleKafkaMessage_AutoCommitOnSuccessLeavesCommitToClient(t *testing.T) {
	t.Parallel()
	committer := &fakeKafkaCommitter{}
	producer := &fakeKafkaProducer{}

	ok := settleKafkaMessage(logging_entry(), &config.Config{KafkaAutoCommit: true},
		testTopicMessage(nil), committer, producer, nil, false)

	assert.True(t, ok, "a completed pipeline counts as processed")
	assert.Zero(t, committer.commits, "librdkafka owns the offset in auto-commit mode")
}

// A nil committer must not panic: kafka.MessageHandler delivers a nil consumer
// in tests and the adapter keeps it nil rather than wrapping a nil pointer.
func TestSettleKafkaMessage_NilCommitterDoesNotPanic(t *testing.T) {
	t.Parallel()
	producer := &fakeKafkaProducer{}

	ok := settleKafkaMessage(logging_entry(), &config.Config{KafkaAutoCommit: false},
		testTopicMessage(nil), nil, producer, assert.AnError, false)

	assert.False(t, ok)
	assert.Empty(t, producer.messages, "without a committer there is no settlement path")
}

// #893 end to end through the handler: a transient pipeline failure in manual
// commit mode must requeue and then settle the offset. This is the wiring the
// deployment never exercised — with auto-commit on, the 15 October files were
// acked by librdkafka and left no status row.
func TestProcessReport_TransientFailureDrivesRetryLadder(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	// The test package's singleton may carry KAFKA_AUTO_COMMIT from an earlier
	// test that flipped it; pin the mode this test is about.
	t.Setenv("KAFKA_AUTO_COMMIT", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	require.False(t, config.GetConfig().KafkaAutoCommit)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	kafkaMsg := types.KafkaMsg{
		Request_id:   "wiring-req",
		B64_identity: "dGVzdA==",
		Files:        []string{ts.URL + "/ocp_ros_usage.csv"},
	}
	kafkaMsg.Metadata.Org_id = "org-wiring"
	kafkaMsg.Metadata.Source_id = "src-wiring"
	kafkaMsg.Metadata.Cluster_uuid = "11111111-2222-3333-4444-555555555555"
	kafkaMsg.Metadata.Cluster_alias = "wiring-cluster"
	kafkaMsg.Metadata.Manifest_id = "manifest-wiring-1"

	value, err := json.Marshal(kafkaMsg)
	require.NoError(t, err)
	topic := "hccm.ros.events"
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: 0},
		Value:          value,
	}

	committer := &fakeKafkaCommitter{}
	producer := &fakeKafkaProducer{}
	processReport(context.Background(), msg, committer, producer)

	require.Len(t, producer.messages, 1,
		"the transient failure must be requeued instead of acked (#893)")
	retryCount, found := headerValue(producer.messages[0].Headers, retryCountHeader)
	require.True(t, found)
	assert.Equal(t, "1", retryCount)
	assert.Equal(t, 1, committer.commits,
		"manual commit settles the offset after the requeue was produced")
}
