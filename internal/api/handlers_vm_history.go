package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

// VMRecommendationHistoryResponse wraps paginated VM recommendation history.
type VMRecommendationHistoryResponse struct {
	Meta  Metadata                            `json:"meta"`
	Links Links                               `json:"links"`
	Data  []engine.VMRecommendationHistoryRow `json:"data"`
}

// GetVMRecommendationHistory handles GET /recommendations/openshift/vms/:vm_name/history.
func GetVMRecommendationHistory(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID

	clusterID := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if clusterID == "" {
		clusterID = strings.TrimSpace(c.QueryParam("cluster_id"))
	}
	if clusterID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "cluster_uuid is required"})
	}

	vmName := strings.TrimSpace(c.Param("vm_name"))
	if vmName == "" {
		vmName = strings.TrimSpace(c.QueryParam("vm_name"))
	}
	namespace := strings.TrimSpace(c.QueryParam("namespace"))
	if vmName == "" || namespace == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "vm_name and namespace are required"})
	}

	term := strings.TrimSpace(c.QueryParam("term"))
	if term == "" {
		term = "short_term"
	}
	engineName := strings.TrimSpace(c.QueryParam("engine"))
	if engineName == "" {
		engineName = "cost"
	}

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	if limit <= 0 {
		limit = 20
	}

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	rows, total, listErr := engine.ListVMRecommendationHistory(
		c.Request().Context(), pool, orgID, clusterID, vmName, namespace, term, engineName, limit, offset,
	)
	if listErr != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": listErr.Error()})
	}

	_ = total
	resp := VMRecommendationHistoryResponse{
		Meta: Metadata{
			Count:  int(total),
			Limit:  limit,
			Offset: offset,
		},
		Data: rows,
	}
	if resp.Data == nil {
		resp.Data = []engine.VMRecommendationHistoryRow{}
	}
	return c.JSON(http.StatusOK, resp)
}
