package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

const (
	synthesizedManifestPrefix = "synth-"
	dateInObjectKeyPrefix     = "date="
)

// synthesizedManifestNamespace is the UUID v5 namespace for legacy Kafka messages
// that omit metadata.manifest_id. Distinct from recommendation ID namespaces.
var synthesizedManifestNamespace = uuid.MustParse("8b3e4567-e89b-12d3-a456-426614174032")

func manifestIDFromMsg(kafkaMsg types.KafkaMsg) string {
	if id := strings.TrimSpace(kafkaMsg.Metadata.Manifest_id); id != "" {
		return id
	}
	return synthesizeManifestID(kafkaMsg)
}

// resolveManifestID returns the manifest ID for tracking, synthesizing a
// deterministic fallback when the publisher omitted metadata.manifest_id.
// When synthesis occurs, the ID is written back to kafkaMsg, a warning is
// logged, and rosocp_ingest_manifest_id_synthesized_total is incremented once.
func resolveManifestID(kafkaMsg *types.KafkaMsg, log *logrus.Entry) string {
	if id := strings.TrimSpace(kafkaMsg.Metadata.Manifest_id); id != "" {
		return id
	}
	id := synthesizeManifestID(*kafkaMsg)
	kafkaMsg.Metadata.Manifest_id = id
	scopeKey := manifestScopeKey(*kafkaMsg)
	IngestManifestIDSynthesized.Inc()
	log.WithFields(logrus.Fields{
		"synthesized_manifest_id": id,
		"manifest_scope_key":      scopeKey,
	}).Warn("Kafka message omitted metadata.manifest_id; using synthesized manifest ID for per-file tracking")
	return id
}

// synthesizeManifestID builds a deterministic manifest ID for legacy Kafka messages
// that omit metadata.manifest_id. ADR-0050: UUID v5 over (org_id, cluster_uuid, scope_key).
func synthesizeManifestID(kafkaMsg types.KafkaMsg) string {
	scopeKey := manifestScopeKey(kafkaMsg)
	name := fmt.Sprintf("%s:%s:%s", kafkaMsg.Metadata.Org_id, kafkaMsg.Metadata.Cluster_uuid, scopeKey)
	id := uuid.NewSHA1(synthesizedManifestNamespace, []byte(name)).String()
	return synthesizedManifestPrefix + id
}

// manifestScopeKey derives the date or payload fingerprint used in manifest synthesis.
// Prefer date=YYYY-MM-DD from ROS object_keys; otherwise hash the sorted file list.
func manifestScopeKey(kafkaMsg types.KafkaMsg) string {
	for _, key := range kafkaMsg.Object_keys {
		if date := extractDateFromObjectKey(key); date != "" {
			return date
		}
	}
	return payloadFingerprint(kafkaMsg)
}

func extractDateFromObjectKey(key string) string {
	idx := strings.Index(key, dateInObjectKeyPrefix)
	if idx < 0 {
		return ""
	}
	rest := key[idx+len(dateInObjectKeyPrefix):]
	if end := strings.IndexAny(rest, "/\\"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func payloadFingerprint(kafkaMsg types.KafkaMsg) string {
	keys := kafkaMsg.Object_keys
	if len(keys) == 0 {
		keys = make([]string, len(kafkaMsg.Files))
		copy(keys, kafkaMsg.Files)
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:8])
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
	expected := kafkaMsg.Metadata.Expected_files
	if len(expected) == 0 {
		expected = make([]string, len(kafkaMsg.Files))
		for i, fileURL := range kafkaMsg.Files {
			expected[i] = filenameForFileIndex(kafkaMsg, fileURL, i)
		}
	}
	if err := model.EnsureReportFileExpectations(
		ctx, pool, manifestID,
		kafkaMsg.Metadata.Cluster_uuid,
		kafkaMsg.Metadata.Org_id,
		expected,
		reportTypeForFilename,
	); err != nil {
		return err
	}
	notifySynthManifestFileActivity(manifestID)
	return nil
}

func shouldSkipProcessedFile(ctx context.Context, pool *pgxpool.Pool, manifestID, filename string) bool {
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
	if err := model.MarkReportFileProcessing(
		ctx, pool, manifestID,
		kafkaMsg.Metadata.Cluster_uuid,
		kafkaMsg.Metadata.Org_id,
		filename, reportType,
	); err != nil {
		log.Errorf("failed to mark report_file_status processing for %s: %v", filename, err)
		return
	}
	notifySynthManifestFileActivity(manifestID)
}
