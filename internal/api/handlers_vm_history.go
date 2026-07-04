package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
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
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	clusterID := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if clusterID == "" {
		clusterID = strings.TrimSpace(c.QueryParam("cluster_id"))
	}
	if clusterID == "" {
		clusterID = queryparams.FirstFilter(c, "cluster")
	}
	if clusterID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "cluster_uuid is required"})
	}

	vmName := strings.TrimSpace(c.Param("vm_name"))
	if vmName == "" {
		vmName = strings.TrimSpace(c.QueryParam("vm_name"))
	}
	namespace := strings.TrimSpace(c.QueryParam("namespace"))
	if namespace == "" {
		namespace = queryparams.FirstFilter(c, "project")
	}
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

	responseFormat, formatErr := listoptions.ResolveResponseFormat(c.Request().Header.Get("Accept"), c.QueryParam("format"))
	if formatErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": formatErr.Error()})
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

	ctx := c.Request().Context()
	allClusters, clusterErr := getClustersForOrg(ctx, orgID)
	if clusterErr != nil {
		hlog.Errorf("GetVMRecommendationHistory: resolve clusters: %v", clusterErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if !clusterAllowed(allowedClusters, clusterID) {
		setRecommendationNoStore(c)
		if responseFormat == listoptions.ResponseFormatCSV {
			return streamCSV(c, csvFilename("vm-recommendation-history"), func(ctx context.Context, w io.Writer) error {
				return generateVMHistoryCSV(ctx, w, nil)
			})
		}
		return c.JSON(http.StatusOK, VMRecommendationHistoryResponse{
			Meta: Metadata{Count: 0, Limit: limit, Offset: offset},
			Data: []engine.VMRecommendationHistoryRow{},
		})
	}

	rows, total, listErr := engine.ListVMRecommendationHistory(
		ctx, pool, orgID, clusterID, vmName, namespace, term, engineName, limit, offset,
	)
	if listErr != nil {
		hlog.Errorf("GetVMRecommendationHistory: query: %v", listErr)
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": "unable to fetch records from database"})
	}

	setRecommendationNoStore(c)
	if responseFormat == listoptions.ResponseFormatCSV {
		if rows == nil {
			rows = []engine.VMRecommendationHistoryRow{}
		}
		return streamCSV(c, csvFilename("vm-recommendation-history"), func(ctx context.Context, w io.Writer) error {
			return generateVMHistoryCSV(ctx, w, rows)
		})
	}

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
