package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// adoptionToleranceFraction uses the same 5% tolerance as quality.go's DetectAdoption.
const adoptionToleranceFraction = 0.05

// FindAdoptedContainers compares each new recommendation's current resource values
// against the PRIOR recommendation values (from oldRecs). If the current CPU
// request and memory request are both within 5% of the prior recommendation,
// the recommendation is considered "adopted" by the user.
//
// Returns the set of containerKeys where adoption was detected.
func FindAdoptedContainers(results []ContainerRec, oldRecs map[containerKey]OldRecommendation) []containerKey {
	var adopted []containerKey
	seen := make(map[containerKey]bool)

	for _, rec := range results {
		key := containerKey{
			Namespace:     rec.Namespace,
			Workload:      rec.Workload,
			WorkloadType:  rec.WorkloadType,
			ContainerName: rec.ContainerName,
		}
		if seen[key] {
			continue
		}

		old, ok := oldRecs[key]
		if !ok || old.RecCPURequestMC == 0 || old.RecMemRequestKiB == 0 {
			continue
		}

		if DetectAdoption(rec.CurrentCPURequestMC, rec.CurrentMemRequestKiB, old.RecCPURequestMC, old.RecMemRequestKiB) {
			adopted = append(adopted, key)
			seen[key] = true
		}
	}
	return adopted
}

// MarkAdopted updates recommendation_sets to set recommendation_applied_at = NOW()
// for the given containers, and appends NotifRecApplied to notification_codes.
func MarkAdopted(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, keys []containerKey) error {
	if len(keys) == 0 {
		return nil
	}

	now := time.Now().UTC()
	notifCode := int16(NotifRecApplied)

	namespaces := make([]string, len(keys))
	workloads := make([]string, len(keys))
	containers := make([]string, len(keys))
	for i, key := range keys {
		namespaces[i] = key.Namespace
		workloads[i] = key.Workload
		containers[i] = key.ContainerName
	}

	tag, err := pool.Exec(ctx, `
		UPDATE recommendation_sets rs
		SET recommendation_applied_at = $1,
			notification_codes = array_append(
				array_remove(notification_codes, $4::smallint),
				$4::smallint
			)
		FROM unnest($5::text[], $6::text[], $7::text[]) AS t(namespace, workload, container_name)
		WHERE rs.org_id = $2 AND rs.cluster_uuid = $3
			AND rs.namespace = t.namespace
			AND rs.workload = t.workload
			AND rs.container_name = t.container_name
			AND rs.recommendation_applied_at IS NULL`,
		now, orgID, clusterUUID, notifCode, namespaces, workloads, containers,
	)
	if err != nil {
		return fmt.Errorf("batch mark adopted: %w", err)
	}
	if tag.RowsAffected() > 0 {
		logging.ForOrg(orgID, clusterUUID).Infof("adoption: marked %d container(s) adopted", tag.RowsAffected())
	}
	return nil
}
