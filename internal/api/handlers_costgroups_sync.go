package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/costgroups"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// PostCostGroupsSync receives admin-curated platform namespaces from
// Koku (#665 Child build, #675). Mirrors PostTagsSync: TokenReview bearer
// auth, org target validation, replace-semantics upsert, updated count.
// Receipt is ungated beyond auth — the zombie rule prefers synced rows
// when present and compiled defaults otherwise.
func PostCostGroupsSync(c echo.Context) error {
	saName, authErr := validateInternalTagsAuth(c)
	if authErr != nil {
		return authErr
	}

	var req costgroups.SyncRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": "invalid JSON request body",
		})
	}
	if err := costgroups.ValidateSyncRequest(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "bad_request",
			"message": err.Error(),
		})
	}
	if orgErr := validateInternalOrgTarget(req.OrgID); orgErr != nil {
		return orgErr
	}
	auditInternalEndpoint(c, "POST /internal/cost-groups/sync", req.OrgID, saName, "sync_cost_groups")

	svc := costgroups.NewSyncService(database.GetPool())
	updated, err := svc.SyncOrgCostGroups(c.Request().Context(), req)
	if err != nil {
		hlog := requestLogger(c, req.OrgID)
		hlog.Errorf("cost-groups sync failed: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"status":  "error",
			"message": "failed to sync cost groups",
		})
	}

	return c.JSON(http.StatusOK, costgroups.SyncResponse{Updated: updated})
}
