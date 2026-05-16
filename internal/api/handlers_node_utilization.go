package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// GetNodeUtilizationRecs handles GET /recommendations/openshift/nodes/utilization.
func GetNodeUtilizationRecs(c echo.Context) error {
	XRHID := c.Get("Identity").(identity.XRHID)
	orgID := XRHID.Identity.OrgID

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 10
	}
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	if offset < 0 {
		offset = 0
	}

	pool := database.GetPool()
	if pool == nil {
		log.Warnf("GetNodeUtilizationRecs: database pool unavailable")
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := context.Background()

	clusterFilter := c.QueryParam("cluster_uuid")
	nodeFilter := c.QueryParam("node")
	termFilter := c.QueryParam("term")
	underutilFilter := c.QueryParam("is_underutilized")
	overcommitFilter := c.QueryParam("is_overcommitted")

	query := `
		SELECT nr.node, nr.cluster_uuid, COALESCE(nr.term, 'medium'),
			COALESCE(nr.cpu_util_p50, 0), COALESCE(nr.cpu_util_p95, 0),
			COALESCE(nr.mem_util_p50, 0), COALESCE(nr.mem_util_p95, 0),
			COALESCE(nr.cpu_overcommit_ratio, 0),
			COALESCE(nr.is_underutilized, false), COALESCE(nr.is_overcommitted, false),
			nr.stranded_resource, COALESCE(nr.pod_count, 0),
			COALESCE(nr.trend_slope, 0), COALESCE(nr.notification_codes, '{}'),
			COALESCE(nr.updated_at, 'epoch'::timestamptz)
		FROM node_recommendations nr
		JOIN clusters c ON nr.cluster_uuid::text = c.cluster_uuid::text
		JOIN rh_accounts a ON c.tenant_id = a.id
		WHERE a.org_id = $1`

	args := []interface{}{orgID}
	argIdx := 2

	if clusterFilter != "" {
		query += " AND nr.cluster_uuid = $" + strconv.Itoa(argIdx)
		args = append(args, clusterFilter)
		argIdx++
	}
	if nodeFilter != "" {
		query += " AND nr.node = $" + strconv.Itoa(argIdx)
		args = append(args, nodeFilter)
		argIdx++
	}
	if termFilter != "" {
		query += " AND nr.term = $" + strconv.Itoa(argIdx)
		args = append(args, termFilter)
		argIdx++
	}
	if underutilFilter == "true" {
		query += " AND nr.is_underutilized = true"
	} else if underutilFilter == "false" {
		query += " AND nr.is_underutilized = false"
	}
	if overcommitFilter == "true" {
		query += " AND nr.is_overcommitted = true"
	} else if overcommitFilter == "false" {
		query += " AND nr.is_overcommitted = false"
	}

	query += " ORDER BY nr.node, nr.term"

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		log.Warnf("GetNodeUtilizationRecs: query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load node utilization recommendations",
		})
	}
	defer rows.Close()

	var allRecs []model.NodeUtilizationRec
	var scanErrors int
	for rows.Next() {
		var rec model.NodeUtilizationRec
		var codes []int16
		var updatedAt time.Time

		err := rows.Scan(
			&rec.Node, &rec.ClusterUUID, &rec.Term,
			&rec.CPUUtilP50, &rec.CPUUtilP95,
			&rec.MemUtilP50, &rec.MemUtilP95,
			&rec.CPUOvercommitRatio,
			&rec.IsUnderutilized, &rec.IsOvercommitted,
			&rec.StrandedResource, &rec.PodCount,
			&rec.TrendSlope, &codes,
			&updatedAt,
		)
		if err != nil {
			scanErrors++
			log.Warnf("GetNodeUtilizationRecs: scan failed (skipping row): %v", err)
			continue
		}

		rec.RecommendationType = "cpu_memory_utilization"
		rec.Notifications = notifications.MapToKruizeFormat(codes)
		rec.UpdatedAt = updatedAt.Format(time.RFC3339)
		allRecs = append(allRecs, rec)
	}
	if err := rows.Err(); err != nil {
		log.Warnf("GetNodeUtilizationRecs: rows iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load node utilization recommendations",
		})
	}

	if allRecs == nil {
		allRecs = []model.NodeUtilizationRec{}
	}

	totalCount := len(allRecs)
	paged := paginateNodeUtil(allRecs, offset, limit)

	resp := model.NodeUtilizationListResponse{
		Meta:  model.NodeUtilizationMeta{Count: totalCount, Limit: limit, Offset: offset},
		Data:  paged,
		Links: buildUtilLinks(c.Request(), totalCount, limit, offset),
	}
	if scanErrors > 0 {
		rowWord := "rows"
		if scanErrors == 1 {
			rowWord = "row"
		}
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("%d %s could not be read", scanErrors, rowWord))
	}

	return c.JSON(http.StatusOK, resp)
}

func paginateNodeUtil(recs []model.NodeUtilizationRec, offset, limit int) []model.NodeUtilizationRec {
	if offset >= len(recs) {
		return []model.NodeUtilizationRec{}
	}
	end := offset + limit
	if end > len(recs) {
		end = len(recs)
	}
	return recs[offset:end]
}

func buildUtilLinks(r *http.Request, total, limit, offset int) map[string]*string {
	baseURL := *r.URL
	q := baseURL.Query()

	makeLink := func(o int) *string {
		q.Set("offset", strconv.Itoa(o))
		q.Set("limit", strconv.Itoa(limit))
		baseURL.RawQuery = q.Encode()
		s, _ := url.PathUnescape(baseURL.String())
		return &s
	}

	links := map[string]*string{
		"first": makeLink(0),
	}
	if offset+limit < total {
		links["next"] = makeLink(offset + limit)
	} else {
		links["next"] = nil
	}
	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		links["previous"] = makeLink(prev)
	} else {
		links["previous"] = nil
	}
	lastOffset := 0
	if total > 0 {
		lastOffset = ((total - 1) / limit) * limit
	}
	links["last"] = makeLink(lastOffset)
	return links
}
