package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// NodeGPUTimeslicingHistoryResponse wraps paginated node GPU time-slicing history.
type NodeGPUTimeslicingHistoryResponse struct {
	Meta  Metadata                                        `json:"meta"`
	Links Links                                           `json:"links"`
	Data  []model.NodeGPUTimeslicingRecommendationHistory `json:"data"`
}

// GetNodeGPUTimeslicingRecommendationHistory handles
// GET /recommendations/openshift/gpu/timeslicing/history.
func GetNodeGPUTimeslicingRecommendationHistory(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	clusterUUID := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if clusterUUID == "" {
		clusterUUID = strings.TrimSpace(c.QueryParam("cluster_id"))
	}
	if clusterUUID == "" {
		clusterUUID = queryparams.FirstFilter(c, "cluster")
	}
	if clusterUUID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "cluster_uuid is required"})
	}

	nodeName := strings.TrimSpace(c.QueryParam("node_name"))
	if nodeName == "" {
		nodeName = queryparams.FirstFilter(c, "node")
	}
	if nodeName == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "node_name is required"})
	}

	gpuModel := strings.TrimSpace(c.QueryParam("gpu_model"))
	if gpuModel == "" {
		gpuModel = queryparams.FirstFilter(c, "gpu_model")
	}

	term := strings.TrimSpace(c.QueryParam("term"))
	if term == "" {
		term = queryparams.FirstFilter(c, "term")
	}
	if term != "" {
		normalized, termErr := queryparams.NormalizeRecommendationTermFilter(term)
		if termErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termErr.Error()})
		}
		term = normalized
	}

	orderCol, orderDir, orderErr := queryparams.ParseOrderBy(
		c, engine.NodeGPUTimeslicingHistoryOrderBy, "recorded_at", listoptions.OrderDesc,
	)
	if orderErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": orderErr.Error()})
	}

	limit, offset, pageErr := listoptions.ParsePagination(c, 20)
	if pageErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": pageErr.Error()})
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
		hlog.Errorf("GetNodeGPUTimeslicingRecommendationHistory: resolve clusters: %v", clusterErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if !clusterAllowed(allowedClusters, clusterUUID) {
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, NodeGPUTimeslicingHistoryResponse{
			Meta: Metadata{Count: 0, Limit: limit, Offset: offset},
			Data: []model.NodeGPUTimeslicingRecommendationHistory{},
		})
	}

	var rows []model.NodeGPUTimeslicingRecommendationHistory
	var total int64
	listErr := db.WithHeavyStatementTimeout(ctx, pool, func(ctx context.Context, q db.QueryRower) error {
		var innerErr error
		rows, total, innerErr = engine.ListNodeGPUTimeslicingRecommendationHistory(
			ctx, q, orgID, clusterUUID, nodeName, gpuModel, term,
			orderCol, orderDir, limit, offset,
		)
		return innerErr
	})
	if listErr != nil {
		hlog.Errorf("GetNodeGPUTimeslicingRecommendationHistory: query: %v", listErr)
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": "unable to fetch records from database"})
	}

	setRecommendationNoStore(c)
	resp := NodeGPUTimeslicingHistoryResponse{
		Meta: Metadata{
			Count:  int(total),
			Limit:  limit,
			Offset: offset,
		},
		Data: rows,
	}
	if resp.Data == nil {
		resp.Data = []model.NodeGPUTimeslicingRecommendationHistory{}
	}
	return c.JSON(http.StatusOK, resp)
}
