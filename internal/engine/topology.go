// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

// ClusterTopologyForRun reads the stored W0 classification for one
// recommendation run. It never fails: unreadable state (missing row,
// pre-migration database, query error) degrades to Unknown with a warning,
// so topology can never break a recommendation run.
func ClusterTopologyForRun(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) topology.ClusterTopology {
	topo, err := pgrec.ReadClusterTopology(ctx, pool, orgID, clusterUUID)
	if err != nil {
		logging.ForOrg(orgID, clusterUUID).Warnf("topology read failed, defaulting unknown: %v", err)
		return topology.TopologyUnknown
	}
	return topo
}
