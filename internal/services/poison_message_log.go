package services

import (
	"fmt"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

const poisonPayloadPreviewBytes = 256

func logPoisonMessage(log *logrus.Entry, msg *kafka.Message, reason string, kafkaMsg *types.KafkaMsg) {
	fields := logrus.Fields{
		"reason":            reason,
		"payload_size_bytes": len(msg.Value),
	}
	if kafkaMsg != nil {
		if kafkaMsg.Request_id != "" {
			fields["request_id"] = kafkaMsg.Request_id
		}
		if kafkaMsg.Metadata.Org_id != "" {
			fields["org_id"] = kafkaMsg.Metadata.Org_id
		}
		if kafkaMsg.Metadata.Cluster_uuid != "" {
			fields["cluster_uuid"] = kafkaMsg.Metadata.Cluster_uuid
		}
	}
	fields["error_class"] = reason

	entry := log.WithFields(fields)
	dlqTopic := config.GetConfig().KafkaDLQTopic
	if dlqTopic == "" {
		dlqTopic = "hccm.ros.events.dlq"
	}
	msgText := fmt.Sprintf(
		"committing poison message (partition=%s, reason=%s); full payload available on DLQ topic %s",
		msg.TopicPartition, reason, dlqTopic,
	)

	if config.GetConfig().LogPoisonPayload && len(msg.Value) > 0 {
		preview := string(msg.Value)
		if len(preview) > poisonPayloadPreviewBytes {
			preview = preview[:poisonPayloadPreviewBytes] + "…(truncated)"
		}
		preview = strings.ReplaceAll(preview, "\n", " ")
		entry = entry.WithField("payload_preview", preview)
	}

	entry.Warn(msgText)
}
