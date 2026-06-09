package services

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

func manifestIDFromMsg(kafkaMsg types.KafkaMsg) string {
	return strings.TrimSpace(kafkaMsg.Metadata.Manifest_id)
}

func filenameForFileIndex(kafkaMsg types.KafkaMsg, fileURL string, index int) string {
	if index >= 0 && index < len(kafkaMsg.Object_keys) && kafkaMsg.Object_keys[index] != "" {
		return filepath.Base(kafkaMsg.Object_keys[index])
	}
	return filepath.Base(fileURL)
}

func reportTypeForFilename(filename string) string {
	return string(utils.DetermineCSVType(filename))
}

func ensureManifestExpectations(ctx context.Context, pool *pgxpool.Pool, kafkaMsg types.KafkaMsg) error {
	manifestID := manifestIDFromMsg(kafkaMsg)
	if manifestID == "" {
		return nil
	}
	expected := kafkaMsg.Metadata.Expected_files
	if len(expected) == 0 {
		expected = make([]string, len(kafkaMsg.Files))
		for i, fileURL := range kafkaMsg.Files {
			expected[i] = filenameForFileIndex(kafkaMsg, fileURL, i)
		}
	}
	return model.EnsureReportFileExpectations(
		ctx, pool, manifestID,
		kafkaMsg.Metadata.Cluster_uuid,
		kafkaMsg.Metadata.Org_id,
		expected,
		reportTypeForFilename,
	)
}

func shouldSkipProcessedFile(ctx context.Context, pool *pgxpool.Pool, manifestID, filename string) bool {
	if manifestID == "" {
		return false
	}
	status, err := model.GetReportFileStatus(ctx, pool, manifestID, filename)
	if err != nil {
		return false
	}
	return status == model.ReportFileDone
}

func recordFileFailure(
	log *logrus.Entry,
	orgID, clusterID, reportType, errorClass string,
) {
	IngestionFileFailures.WithLabelValues(orgID, clusterID, reportType, errorClass).Inc()
	log.WithFields(logrus.Fields{
		"org_id":       orgID,
		"cluster_uuid": clusterID,
		"report_type":  reportType,
		"error_class":  errorClass,
	}).Error("ros ingestion file failure")
}

func classifyIngestionError(err error) string {
	if err == nil {
		return "unknown"
	}
	if isTransientKafkaProcessingError(err) {
		return "transient"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "status code"), strings.Contains(msg, "fetch"):
		return "fetch"
	case strings.Contains(msg, "parse"), strings.Contains(msg, "csv"):
		return "parse"
	case strings.Contains(msg, "digest"), strings.Contains(msg, "ingest"):
		return "ingest"
	default:
		return "other"
	}
}

func handlePermanentFileError(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	filename, reportType string,
	err error,
) {
	errMsg := err.Error()
	if markErr := model.MarkReportFileFailed(ctx, pool, manifestIDFromMsg(kafkaMsg), filename, errMsg); markErr != nil {
		log.Errorf("failed to record report_file_status failure for %s: %v", filename, markErr)
	}
	recordFileFailure(log, kafkaMsg.Metadata.Org_id, kafkaMsg.Metadata.Cluster_uuid, reportType, classifyIngestionError(err))
}

func markFileDone(ctx context.Context, pool *pgxpool.Pool, log *logrus.Entry, manifestID, filename string) {
	if manifestID == "" {
		return
	}
	if err := model.MarkReportFileDone(ctx, pool, manifestID, filename); err != nil {
		log.Errorf("failed to mark report_file_status done for %s: %v", filename, err)
	}
}

func markFileProcessing(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *logrus.Entry,
	kafkaMsg types.KafkaMsg,
	filename, reportType string,
) {
	manifestID := manifestIDFromMsg(kafkaMsg)
	if manifestID == "" {
		return
	}
	if err := model.MarkReportFileProcessing(
		ctx, pool, manifestID,
		kafkaMsg.Metadata.Cluster_uuid,
		kafkaMsg.Metadata.Org_id,
		filename, reportType,
	); err != nil {
		log.Errorf("failed to mark report_file_status processing for %s: %v", filename, err)
	}
}
