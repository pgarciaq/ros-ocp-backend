package api

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
)

// VMRecommendationHistoryResponse wraps paginated VM recommendation history.
type VMRecommendationHistoryResponse struct {
	Meta  Metadata                        `json:"meta"`
	Links Links                           `json:"links"`
	Data  []vm.VMRecommendationHistoryRow `json:"data"`
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

	// Accept both published spellings in any case and translate once at the
	// bind: vm_recommendation_history stores the `_term` form (#959). Before
	// this, any unknown value silently returned an empty page.
	termRaw := queryparams.FirstFilter(c, "term")
	if termRaw == "" {
		termRaw = "short_term"
	}
	termBare, termErr := queryparams.NormalizeRecommendationTermFilter(termRaw)
	if termErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termErr.Error()})
	}
	term := queryparams.TermStorageName("vm", termBare)
	engineName := strings.TrimSpace(c.QueryParam("engine"))
	if engineName == "" {
		engineName = "cost"
	}

	responseFormat, formatErr := listoptions.ResolveResponseFormat(c.Request().Header.Get("Accept"), c.QueryParam("format"))
	if formatErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": formatErr.Error()})
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

	// #967: history rows carry cluster_id, so this is a union surface — an
	// alias resolving to several readable clusters returns all of them, and a
	// lookup failure is a 503 rather than an empty page (#953).
	scopedClusters, scopeErr := clusterScopeForFilter(ctx, pool, orgID, userPerms, clusterID)
	if scopeErr != nil {
		hlog.Errorf("GetVMRecommendationHistory: resolve cluster filter: %v", scopeErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	if len(scopedClusters) == 0 {
		setRecommendationNoStore(c)
		if responseFormat == listoptions.ResponseFormatCSV {
			return streamCSV(c, csvFilename("vm-recommendation-history"), func(ctx context.Context, w io.Writer) error {
				return generateVMHistoryCSV(w, nil)
			})
		}
		return c.JSON(http.StatusOK, VMRecommendationHistoryResponse{
			Meta: Metadata{Count: 0, Limit: limit, Offset: offset},
			Data: []vm.VMRecommendationHistoryRow{},
		})
	}

	rows, total, listErr := vm.ListVMRecommendationHistory(
		ctx, pool, orgID, scopedClusters, vmName, namespace, term, engineName, limit, offset,
	)
	if listErr != nil {
		hlog.Errorf("GetVMRecommendationHistory: query: %v", listErr)
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": "unable to fetch records from database"})
	}

	setRecommendationNoStore(c)
	if responseFormat == listoptions.ResponseFormatCSV {
		if rows == nil {
			rows = []vm.VMRecommendationHistoryRow{}
		}
		return streamCSV(c, csvFilename("vm-recommendation-history"), func(ctx context.Context, w io.Writer) error {
			return generateVMHistoryCSV(w, rows)
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
		resp.Data = []vm.VMRecommendationHistoryRow{}
	}
	return c.JSON(http.StatusOK, resp)
}
