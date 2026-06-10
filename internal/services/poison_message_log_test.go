package services

import (
	"bytes"
	"strings"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestLogPoisonMessage_OmitsPayloadByDefault(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_LOG_POISON_PAYLOAD", "false")
	_ = config.GetConfig()

	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)
	log.SetLevel(logrus.DebugLevel)
	entry := log.WithField("component", "test")

	msg := &kafka.Message{
		Value:          []byte(`{"metadata":{"org_id":"123","cluster_uuid":"abc"},"secret":"PII"}`),
		TopicPartition: kafka.TopicPartition{Topic: strPtr("upload"), Partition: 1},
	}
	kafkaMsg := &types.KafkaMsg{Request_id: "req-1"}
	kafkaMsg.Metadata.Org_id = "123"
	kafkaMsg.Metadata.Cluster_uuid = "abc"

	logPoisonMessage(entry, msg, "validation failed", kafkaMsg)

	out := buf.String()
	assert.Contains(t, out, "req-1")
	assert.Contains(t, out, "123")
	assert.Contains(t, out, "abc")
	assert.Contains(t, out, "DLQ")
	assert.NotContains(t, out, "PII")
	assert.NotContains(t, out, "payload_preview")
}

func TestLogPoisonMessage_IncludesTruncatedPreviewWhenEnabled(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_LOG_POISON_PAYLOAD", "true")
	_ = config.GetConfig()

	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)
	log.SetLevel(logrus.DebugLevel)
	entry := log.WithField("component", "test")

	payload := strings.Repeat("x", 300)
	msg := &kafka.Message{Value: []byte(payload), TopicPartition: kafka.TopicPartition{Partition: 0}}

	logPoisonMessage(entry, msg, "invalid JSON", nil)

	out := buf.String()
	assert.Contains(t, out, "payload_preview")
	assert.NotContains(t, out, strings.Repeat("x", 300))
}

func strPtr(s string) *string { return &s }
