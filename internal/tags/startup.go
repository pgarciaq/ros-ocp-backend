package tags

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// RunStartupHealthCheck verifies Koku tag table access when ROS_TAGS_SOURCE=db.
// On failure it logs a clear message and disables tag filtering for the process.
func RunStartupHealthCheck(ctx context.Context) {
	if !config.TagsFeatureEnabled() || config.TagsSource() != "db" {
		return
	}

	pool := database.GetPool()
	if pool == nil {
		logging.GetLogger().Error(
			"Tag filtering (ROS_TAGS_SOURCE=db) is enabled but the database pool is unavailable. Disabling tag filtering.",
		)
		config.DisableTagsFeature()
		return
	}

	orgID, schema := probeOrgSchema(ctx, pool)
	if err := VerifyDBAccess(ctx, pool); err != nil {
		logging.GetLogger().Errorf(
			"Tag filtering (ROS_TAGS_SOURCE=db) is enabled but Koku tag tables are not accessible in schema %s (org_id=%s): %v. "+
				"Disabling tag filtering. Ensure ROS connects to the same PostgreSQL instance as Koku.",
			schema, orgID, err,
		)
		config.DisableTagsFeature()
	}
}

func probeOrgSchema(ctx context.Context, pool *pgxpool.Pool) (orgID, schema string) {
	_ = pool.QueryRow(ctx, `SELECT org_id FROM rh_accounts ORDER BY id LIMIT 1`).Scan(&orgID)
	if orgID != "" {
		if s, err := TenantSchema(orgID); err == nil {
			schema = s
		}
	}
	if schema == "" {
		schema = "org<unknown>"
	}
	return orgID, schema
}
