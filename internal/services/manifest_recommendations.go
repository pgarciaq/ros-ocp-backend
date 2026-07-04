package services

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

var (
	runManifestRecommendationsHook func(context.Context, *pgxpool.Pool, types.KafkaMsg) error
	runVMRecommendationsHook       func(context.Context, types.KafkaMsg) error
)

// runManifestRecommendations executes recommendation engines after all expected
// manifest files have been ingested successfully (ADR-0166).
func runManifestRecommendations(ctx context.Context, pool *pgxpool.Pool, kafkaMsg types.KafkaMsg) error {
	if runManifestRecommendationsHook != nil {
		return runManifestRecommendationsHook(ctx, pool, kafkaMsg)
	}
	manifestID := manifestIDFromMsg(kafkaMsg)
	complete, err := model.IsManifestIngestionComplete(ctx, pool, manifestID)
	if err != nil {
		return err
	}
	if !complete {
		log := logging.ForOrg(kafkaMsg.Metadata.Org_id, kafkaMsg.Metadata.Cluster_uuid)
		log.Infof("manifest %s incomplete — deferring recommendation engine", manifestID)
		return nil
	}

	reportTypes, err := model.CompletedReportTypes(ctx, pool, manifestID)
	if err != nil {
		return err
	}
	typeSet := make(map[string]struct{}, len(reportTypes))
	for _, rt := range reportTypes {
		typeSet[rt] = struct{}{}
	}

	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)
	log.Infof("manifest %s complete — running deferred recommendation engines", manifestID)

	for rt := range typeSet {
		switch types.PayloadType(rt) {
		case types.PayloadTypeContainer:
			if err := runContainerRecommendations(ctx, kafkaMsg); err != nil {
				return err
			}
		case types.PayloadTypeNamespace:
			if err := runNamespaceRecommendations(ctx, kafkaMsg); err != nil {
				return err
			}
		case types.PayloadTypeStorage:
			if err := runStorageRecommendations(ctx, kafkaMsg); err != nil {
				return err
			}
		case types.PayloadTypeSnapshot:
			if err := runSnapshotRecommendations(ctx, kafkaMsg); err != nil {
				return err
			}
		case types.PayloadTypeClusterQuota:
			if err := runClusterQuotaRecommendations(ctx, kafkaMsg); err != nil {
				return err
			}
		case types.PayloadTypeVM, types.PayloadTypeVMGPU:
			if plugin.EnabledFor("vm") {
				if err := runVMRecommendations(ctx, kafkaMsg); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func runClusterQuotaRecommendations(ctx context.Context, kafkaMsg types.KafkaMsg) error {
	pool := db.GetPool()
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)
	if err := engine.RunClusterQuotaRecommendations(ctx, pool, orgID, clusterUUID); err != nil {
		log.Errorf("native cluster-quota engine: recommendation failed: %v", err)
		return err
	}
	return nil
}

func runVMRecommendations(ctx context.Context, kafkaMsg types.KafkaMsg) error {
	if runVMRecommendationsHook != nil {
		return runVMRecommendationsHook(ctx, kafkaMsg)
	}
	pool := db.GetPool()
	orgID := kafkaMsg.Metadata.Org_id
	clusterUUID := kafkaMsg.Metadata.Cluster_uuid
	log := logging.ForOrg(orgID, clusterUUID)
	if !plugin.EnabledFor("vm") {
		return nil
	}
	clusterID, parseErr := uuid.Parse(clusterUUID)
	if parseErr != nil {
		return parseErr
	}
	if recErr := engine.RunVMRecommendations(ctx, pool, orgID, clusterID, engine.VMRecConfigResolved()); recErr != nil {
		log.Errorf("native VM engine: recommendations failed: %v", recErr)
		return recErr
	}
	return nil
}
