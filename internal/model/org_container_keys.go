package model

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// OrgContainerKey mirrors org_container_keys for ORM reads when needed.
type OrgContainerKey struct {
	OrgID         string `gorm:"column:org_id"`
	ClusterUUID   string `gorm:"column:cluster_uuid"`
	Namespace     string `gorm:"column:namespace"`
	Workload      string `gorm:"column:workload"`
	WorkloadType  string `gorm:"column:workload_type"`
	ContainerName string `gorm:"column:container_name"`
	IsStale       bool   `gorm:"column:is_stale"`
}

func (OrgContainerKey) TableName() string {
	return "org_container_keys"
}

// RefreshOrgContainerKeys upserts active container keys and removes stale entries.
func RefreshOrgContainerKeys(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	return pgrec.RefreshOrgContainerKeys(ctx, pool, orgID)
}

// RefreshOrgContainerKeysTx upserts all container keys (stale and non-stale) within
// an existing transaction. The is_stale column tracks whether the most-recently-updated
// recommendation_sets row for each container composite key is stale.
func RefreshOrgContainerKeysTx(ctx context.Context, tx pgx.Tx, orgID string) error {
	return pgrec.RefreshOrgContainerKeysTx(ctx, tx, orgID)
}
