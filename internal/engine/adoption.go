package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
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
	var errs []error
	for _, key := range keys {
		tag, err := pool.Exec(ctx, `
			UPDATE recommendation_sets
			SET recommendation_applied_at = $1,
				notification_codes = array_append(
					array_remove(notification_codes, $5::smallint),
					$5::smallint
				)
			WHERE org_id = $2 AND cluster_uuid = $3
				AND namespace = $4 AND workload = $6 AND container_name = $7
				AND recommendation_applied_at IS NULL`,
			now, orgID, clusterUUID, key.Namespace, int16(NotifRecApplied), key.Workload, key.ContainerName,
		)
		if err != nil {
			log.Warnf("adoption: marking %s/%s/%s: %v", key.Namespace, key.Workload, key.ContainerName, err)
			errs = append(errs, fmt.Errorf("%s/%s/%s: %w", key.Namespace, key.Workload, key.ContainerName, err))
		} else if tag.RowsAffected() > 0 {
			log.Infof("adoption: detected for %s/%s/%s in cluster %s", key.Namespace, key.Workload, key.ContainerName, clusterUUID)
		}
	}
	return errors.Join(errs...)
}
