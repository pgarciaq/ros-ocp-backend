package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// ResolveHCAssociation maps HCP namespaces to hosted cluster IDs (#621
// temporal rule). Defined in pgrec beside SnapshotRow so engine/container
// (which cannot import engine) shares one rule; this wrapper keeps the
// engine-level name stable for callers.
func ResolveHCAssociation(snaps []pgrec.SnapshotRow) map[string]string {
	return pgrec.ResolveHCAssociation(snaps)
}

// LoadHCAssociationForRun resolves the namespace-to-HC map for org+cluster
// over the digest lookback window. Unreadable state degrades to an empty map
// with nil error: callers must never fail a recommendation run on association.
func LoadHCAssociationForRun(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (map[string]string, error) {
	maxLookbackDays := config.GetConfig().MaxLookbackDays
	if maxLookbackDays <= 0 {
		maxLookbackDays = 14
	}
	snaps, err := pgrec.LoadHCPSnapshotsForRun(ctx, pool, orgID, clusterUUID, time.Now().UTC().AddDate(0, 0, -maxLookbackDays))
	if err != nil {
		logging.GetLogger().Warnf("unable to load hcp snapshots (association off): %v", err)
		return map[string]string{}, nil
	}
	return ResolveHCAssociation(snaps), nil
}

// MarkHCPAssociations writes the resolved map onto recommendation_sets (set
// pass) and clears lapsed associations (clear pass), mirroring the has_gpu
// marking precedent. Pre-migration databases (missing column) degrade to
// zero counts with nil error.
func MarkHCPAssociations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, hcMap map[string]string) (associated, cleared int64, err error) {
	if len(hcMap) > 0 {
		namespaces := make([]string, 0, len(hcMap))
		for ns := range hcMap {
			namespaces = append(namespaces, ns)
		}
		// Deterministic placeholder order for test-stable SQL.
		slices.Sort(namespaces)
		args := make([]any, 0, 2+2*len(namespaces))
		args = append(args, orgID, clusterUUID)
		var pairs []string
		for _, ns := range namespaces {
			args = append(args, ns, hcMap[ns])
			pairs = append(pairs, fmt.Sprintf("($%d,$%d)", len(args)-1, len(args)))
		}
		res, execErr := pool.Exec(ctx, fmt.Sprintf(`
			UPDATE recommendation_sets rs SET hosted_cluster_id = m.hc
			FROM (VALUES %s) AS m(ns, hc)
			WHERE rs.org_id = $1 AND rs.cluster_uuid = $2 AND rs.namespace = m.ns
			  AND (rs.hosted_cluster_id IS DISTINCT FROM m.hc)`, strings.Join(pairs, ",")),
			args...)
		if execErr != nil {
			if pgrec.IsUndefinedTable(execErr) {
				return 0, 0, nil
			}
			return 0, 0, fmt.Errorf("mark hcp associations: %w", execErr)
		}
		associated = res.RowsAffected()
		metrics.HCPAssociationTotal.WithLabelValues("associated").Add(float64(associated))

		res, execErr = pool.Exec(ctx, `
			UPDATE recommendation_sets SET hosted_cluster_id = NULL
			WHERE org_id = $1 AND cluster_uuid = $2 AND hosted_cluster_id IS NOT NULL
			  AND NOT (namespace = ANY($3))`, orgID, clusterUUID, namespaces)
		if execErr != nil {
			if pgrec.IsUndefinedTable(execErr) {
				return associated, 0, nil
			}
			return associated, 0, fmt.Errorf("clear lapsed hcp associations: %w", execErr)
		}
		cleared = res.RowsAffected()
	} else {
		res, execErr := pool.Exec(ctx, `
			UPDATE recommendation_sets SET hosted_cluster_id = NULL
			WHERE org_id = $1 AND cluster_uuid = $2 AND hosted_cluster_id IS NOT NULL`,
			orgID, clusterUUID)
		if execErr != nil {
			if pgrec.IsUndefinedTable(execErr) {
				return 0, 0, nil
			}
			return 0, 0, fmt.Errorf("clear hcp associations: %w", execErr)
		}
		cleared = res.RowsAffected()
	}
	if cleared > 0 {
		metrics.HCPAssociationTotal.WithLabelValues("cleared").Add(float64(cleared))
	}
	return associated, cleared, nil
}

// MarkHCPAssociationsForRun loads the window association map and writes it.
// It never fails the run on association I/O: load degrades internally and
// marking errors are returned for the caller to warn-and-continue.
func MarkHCPAssociationsForRun(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (int64, int64, error) {
	hcMap, err := LoadHCAssociationForRun(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return 0, 0, err
	}
	return MarkHCPAssociations(ctx, pool, orgID, clusterUUID, hcMap)
}
