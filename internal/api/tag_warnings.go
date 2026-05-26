package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
)

const (
	tagStatusEndpoint = "GET /api/cost-management/v1/internal/tags/status"
	tagSyncStaleAfter = 6 * time.Hour
)

// buildTagWarnings returns meta.warnings when tag filtering is active, results are empty,
// and the catalog or sync state suggests the filter may be ineffective.
func buildTagWarnings(ctx context.Context, orgID string, requestedTagKeys []string, resultCount int) []string {
	if resultCount > 0 || len(requestedTagKeys) == 0 || !config.TagsFeatureEnabled() {
		return nil
	}

	pool := db.GetPool()
	if pool == nil {
		return nil
	}

	known, err := knownTagKeysForOrg(ctx, pool, orgID)
	if err != nil {
		return nil
	}

	var warnings []string
	for _, key := range requestedTagKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := known[key]; !ok {
			warnings = append(warnings, fmt.Sprintf(
				"Tag filtering is active but no data matches. The requested tag key %q is not in the known tag catalog. Verify tag sync status via %s",
				key, tagStatusEndpoint,
			))
		}
	}

	if config.TagsUsePushSync() {
		if syncWarn := tagSyncWarning(ctx, pool, orgID); syncWarn != "" {
			warnings = append(warnings, syncWarn)
		}
	}

	if len(warnings) == 0 {
		return nil
	}
	return warnings
}

func tagSyncWarning(ctx context.Context, pool *pgxpool.Pool, orgID string) string {
	svc := tags.NewSyncService(pool)
	status, err := svc.GetSyncStatus(ctx, orgID)
	if err != nil || status == nil || status.SyncedAt == nil {
		return fmt.Sprintf(
			"Tag filtering is active but no data matches. Tag push sync has not completed for this organization. Verify tag sync status via %s",
			tagStatusEndpoint,
		)
	}
	if time.Since(*status.SyncedAt) > tagSyncStaleAfter {
		return fmt.Sprintf(
			"Tag filtering is active but no data matches. Tag push sync is stale (last synced %s). Verify tag sync status via %s",
			status.SyncedAt.UTC().Format(time.RFC3339), tagStatusEndpoint,
		)
	}
	return ""
}

func knownTagKeysForOrg(ctx context.Context, pool *pgxpool.Pool, orgID string) (map[string]struct{}, error) {
	known := make(map[string]struct{})

	provider := tags.GetProvider()
	if provider != nil {
		catalog, err := tags.BuildTagCatalog(ctx, provider, orgID)
		if err == nil {
			for _, entry := range catalog {
				if entry.Key != "" {
					known[entry.Key] = struct{}{}
				}
			}
		}
	}

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT jsonb_object_keys(resolved_tags)
		FROM org_container_keys
		WHERE org_id = $1`, orgID,
	)
	if err != nil {
		return known, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return known, err
		}
		if key != "" {
			known[key] = struct{}{}
		}
	}
	return known, rows.Err()
}

func requestedTagKeysFromContext(c echo.Context) []string {
	filters, err := parseTagFiltersFromRequest(c)
	if err != nil || len(filters) == 0 {
		return nil
	}
	keys := make([]string, 0, len(filters))
	seen := make(map[string]struct{}, len(filters))
	for _, f := range filters {
		if f.Key == "" {
			continue
		}
		if _, ok := seen[f.Key]; ok {
			continue
		}
		seen[f.Key] = struct{}{}
		keys = append(keys, f.Key)
	}
	return keys
}

func attachTagWarningsToCollection(resp *Collection, c echo.Context, orgID string, resultCount int) {
	if resp == nil {
		return
	}
	resp.Meta.Warnings = buildTagWarnings(c.Request().Context(), orgID, requestedTagKeysFromContext(c), resultCount)
}

func attachTagWarningsToPVC(resp *PVCRecommendationListResponse, c echo.Context, orgID string, resultCount int) {
	if resp == nil {
		return
	}
	resp.Meta.Warnings = buildTagWarnings(c.Request().Context(), orgID, requestedTagKeysFromContext(c), resultCount)
}

func attachTagWarningsToNodeUtil(resp *model.NodeUtilizationListResponse, c echo.Context, orgID string, resultCount int) {
	if resp == nil {
		return
	}
	tagWarnings := buildTagWarnings(c.Request().Context(), orgID, requestedTagKeysFromContext(c), resultCount)
	if len(tagWarnings) == 0 {
		return
	}
	resp.Meta.Warnings = append(resp.Meta.Warnings, tagWarnings...)
}

func attachTagWarningsToGPUMIG(resp *model.GPUMIGListResponse, c echo.Context, orgID string, resultCount int) {
	if resp == nil {
		return
	}
	tagWarnings := buildTagWarnings(c.Request().Context(), orgID, requestedTagKeysFromContext(c), resultCount)
	if len(tagWarnings) == 0 {
		return
	}
	resp.Meta.Warnings = append(resp.Meta.Warnings, tagWarnings...)
}
